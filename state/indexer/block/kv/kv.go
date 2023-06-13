package kv

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/orderedcode"

	ds "github.com/ipfs/go-datastore"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/pubsub/query"
	"github.com/cometbft/cometbft/libs/pubsub/query/syntax"
	"github.com/cometbft/cometbft/types"

	"github.com/rollkit/rollkit/state/indexer"
	"github.com/rollkit/rollkit/store"
)

var _ indexer.BlockIndexer = (*BlockerIndexer)(nil)

// BlockerIndexer implements a block indexer, indexing BeginBlock and EndBlock
// events with an underlying KV store. Block events are indexed by their height,
// such that matching search criteria returns the respective block height(s).
type BlockerIndexer struct {
	store ds.TxnDatastore

	ctx context.Context
}

func New(ctx context.Context, store ds.TxnDatastore) *BlockerIndexer {
	return &BlockerIndexer{
		store: store,
		ctx:   ctx,
	}
}

// Has returns true if the given height has been indexed. An error is returned
// upon database query failure.
func (idx *BlockerIndexer) Has(height int64) (bool, error) {
	_, err := heightKey(height)
	if err == ds.ErrNotFound {
		return false, nil
	}
	return err == nil, err
}

// Index indexes BeginBlock and EndBlock events for a given block by its height.
// The following is indexed:
//
// primary key: encode(block.height | height) => encode(height)
// FinalizeBlock events: encode(eventType.eventAttr|eventValue|height|finalize_block|eventSeq) => encode(height)
func (idx *BlockerIndexer) Index(bh types.EventDataNewBlockEvents) error {
	batch, err := idx.store.NewTransaction(idx.ctx, false)
	if err != nil {
		return fmt.Errorf("failed to create a new batch for transaction: %w", err)
	}
	defer batch.Discard(idx.ctx)

	height := bh.Height

	// 1. index by height
	key, err := heightKey(height)
	if err != nil {
		return fmt.Errorf("failed to create block height index key: %w", err)
	}
	if err := batch.Put(idx.ctx, ds.NewKey(string(key)), int64ToBytes(height)); err != nil {
		return err
	}

	// 2. index block events
	if err := idx.indexEvents(batch, bh.Events, "finalize_block", height); err != nil {
		return fmt.Errorf("failed to index FinalizeBlock events: %w", err)
	}
	return batch.Commit(idx.ctx)
}

// Search performs a query for block heights that match a given BeginBlock
// and Endblock event search criteria. The given query can match against zero,
// one or more block heights. In the case of height queries, i.e. block.height=H,
// if the height is indexed, that height alone will be returned. An error and
// nil slice is returned. Otherwise, a non-nil slice and nil error is returned.
func (idx *BlockerIndexer) Search(ctx context.Context, q *query.Query) ([]int64, error) {
	results := make([]int64, 0)
	select {
	case <-ctx.Done():
		return results, nil

	default:
	}

	conditions := q.Syntax()

	// conditions to skip because they're handled before "everything else"
	skipIndexes := make([]int, 0)

	var ok bool

	var heightInfo HeightInfo
	// If we are not matching events and block.height occurs more than once, the later value will
	// overwrite the first one.
	conditions, heightInfo, ok = dedupHeight(conditions)

	// Extract ranges. If both upper and lower bounds exist, it's better to get
	// them in order as to not iterate over kvs that are not within range.
	ranges, rangeIndexes, heightRange := indexer.LookForRangesWithHeight(conditions)
	heightInfo.heightRange = heightRange

	// If we have additional constraints and want to query per event
	// attributes, we cannot simply return all blocks for a height.
	// But we remember the height we want to find and forward it to
	// match(). If we only have the height constraint
	// in the query (the second part of the ||), we don't need to query
	// per event conditions and return all events within the height range.
	if ok && heightInfo.onlyHeightEq {
		ok, err := idx.Has(heightInfo.height)
		if err != nil {
			return nil, err
		}

		if ok {
			return []int64{heightInfo.height}, nil
		}

		return results, nil
	}

	var heightsInitialized bool
	filteredHeights := make(map[string][]byte)
	if heightInfo.heightEqIdx != -1 {
		skipIndexes = append(skipIndexes, heightInfo.heightEqIdx)
	}

	if len(ranges) > 0 {
		skipIndexes = append(skipIndexes, rangeIndexes...)

		for _, qr := range ranges {
			// If we have a query range over height and want to still look for
			// specific event values we do not want to simply return all
			// blocks in this height range. We remember the height range info
			// and pass it on to match() to take into account when processing events.
			if qr.Key == types.BlockHeightKey && !heightInfo.onlyHeightRange {
				// If the query contains ranges other than the height then we need to treat the height
				// range when querying the conditions of the other range.
				// Otherwise we can just return all the blocks within the height range (as there is no
				// additional constraint on events)

				continue

			}
			prefix, err := orderedcode.Append(nil, qr.Key)
			if err != nil {
				return nil, fmt.Errorf("failed to create prefix key: %w", err)
			}

			if !heightsInitialized {
				filteredHeights, err = idx.matchRange(ctx, qr, prefix, filteredHeights, true, heightInfo)
				if err != nil {
					return nil, err
				}

				heightsInitialized = true

				// Ignore any remaining conditions if the first condition resulted in no
				// matches (assuming implicit AND operand).
				if len(filteredHeights) == 0 {
					break
				}
			} else {
				filteredHeights, err = idx.matchRange(ctx, qr, prefix, filteredHeights, false, heightInfo)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	// for all other conditions
	for i, c := range conditions {
		if intInSlice(i, skipIndexes) {
			continue
		}

		startKey, err := orderedcode.Append(nil, c.Tag, c.Arg.Value())
		if err != nil {
			return nil, err
		}

		if !heightsInitialized {
			filteredHeights, err = idx.match(ctx, c, startKey, filteredHeights, true, heightInfo)
			if err != nil {
				return nil, err
			}

			heightsInitialized = true

			// Ignore any remaining conditions if the first condition resulted in no
			// matches (assuming implicit AND operand).
			if len(filteredHeights) == 0 {
				break
			}
		} else {
			filteredHeights, err = idx.match(ctx, c, startKey, filteredHeights, false, heightInfo)
			if err != nil {
				return nil, err
			}
		}
	}

	// fetch matching heights
	results = make([]int64, 0, len(filteredHeights))
	resultMap := make(map[int64]struct{})
	for _, hBz := range filteredHeights {
		h := int64FromBytes(hBz)

		ok, err := idx.Has(h)
		if err != nil {
			return nil, err
		}
		if ok {
			if _, ok := resultMap[h]; !ok {
				resultMap[h] = struct{}{}
				results = append(results, h)
			}
		}

		select {
		case <-ctx.Done():
			break

		default:
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })

	return results, nil
}

// matchRange returns all matching block heights that match a given QueryRange
// and start key. An already filtered result (filteredHeights) is provided such
// that any non-intersecting matches are removed.
//
// NOTE: The provided filteredHeights may be empty if no previous condition has
// matched.
func (idx *BlockerIndexer) matchRange(
	ctx context.Context,
	qr indexer.QueryRange,
	startKey []byte,
	filteredHeights map[string][]byte,
	firstRun bool,
	heightInfo HeightInfo,
) (map[string][]byte, error) {

	// A previous match was attempted but resulted in no matches, so we return
	// no matches (assuming AND operand).
	if !firstRun && len(filteredHeights) == 0 {
		return filteredHeights, nil
	}

	tmpHeights := make(map[string][]byte)
	lowerBound := qr.LowerBoundValue()
	upperBound := qr.UpperBoundValue()

	results, err := store.PrefixEntries(ctx, idx.store, string(startKey))
	if err != nil {
		return nil, err
	}

	for result := range results.Next() {
		cont := true

		var (
			v          int64
			eventValue string
			err        error
		)

		if qr.Key == types.BlockHeightKey {
			eventValue, err = parseValueFromPrimaryKey([]byte(result.Entry.Key))
		} else {
			eventValue, err = parseValueFromEventKey([]byte(result.Entry.Key))
		}
		if err != nil {
			continue
		}

		if _, ok := qr.AnyBound().(int64); ok {
			v, err = strconv.ParseInt(eventValue, 10, 64)
			if err != nil {
				continue
			}
			include := true
			if lowerBound != nil && v < lowerBound.(int64) {
				include = false
			}

			if upperBound != nil && v > upperBound.(int64) {
				include = false
			}

			if include {
				tmpHeights[string(result.Entry.Value)] = result.Entry.Value
			}
		}

		select {
		case <-ctx.Done():
			cont = false

		default:
		}

		if !cont {
			break
		}
	}

	if len(tmpHeights) == 0 || firstRun {
		// Either:
		//
		// 1. Regardless if a previous match was attempted, which may have had
		// results, but no match was found for the current condition, then we
		// return no matches (assuming AND operand).
		//
		// 2. A previous match was not attempted, so we return all results.
		return tmpHeights, nil
	}

	// Remove/reduce matches in filteredHashes that were not found in this
	// match (tmpHashes).
	for k := range filteredHeights {
		cont := true

		if tmpHeights[k] == nil {
			delete(filteredHeights, k)

			select {
			case <-ctx.Done():
				cont = false

			default:
			}
		}

		if !cont {
			break
		}
	}

	return filteredHeights, nil
}

// match returns all matching heights that meet a given query condition and start
// key. An already filtered result (filteredHeights) is provided such that any
// non-intersecting matches are removed.
//
// NOTE: The provided filteredHeights may be empty if no previous condition has
// matched.
func (idx *BlockerIndexer) match(
	ctx context.Context,
	c syntax.Condition,
	startKeyBz []byte,
	filteredHeights map[string][]byte,
	firstRun bool,
	heightInfo HeightInfo,
) (map[string][]byte, error) {

	// A previous match was attempted but resulted in no matches, so we return
	// no matches (assuming AND operand).
	if !firstRun && len(filteredHeights) == 0 {
		return filteredHeights, nil
	}

	tmpHeights := make(map[string][]byte)

	switch {
	case c.Op == syntax.TEq:
		results, err := store.PrefixEntries(ctx, idx.store, string(startKeyBz))
		if err != nil {
			return nil, err
		}

		for result := range results.Next() {
			tmpHeights[string(result.Entry.Value)] = result.Entry.Value

			if err := ctx.Err(); err != nil {
				break
			}
		}

	case c.Op == syntax.TExists:

		results, err := store.PrefixEntries(ctx, idx.store, ds.NewKey(c.Tag).String())
		if err != nil {
			return nil, err
		}

		for result := range results.Next() {
			cont := true

			tmpHeights[string(result.Entry.Value)] = result.Entry.Value

			select {
			case <-ctx.Done():
				cont = false

			default:
			}

			if !cont {
				break
			}
		}

	case c.Op == syntax.TContains:

		results, err := store.PrefixEntries(ctx, idx.store, ds.NewKey(c.Tag).String())
		if err != nil {
			return nil, err
		}

		for result := range results.Next() {
			cont := true

			eventValue, err := parseValueFromEventKey([]byte(result.Entry.Key))
			if err != nil {
				continue
			}

			if strings.Contains(eventValue, c.Arg.Value()) {
				tmpHeights[string(result.Entry.Value)] = result.Entry.Value
			}

			select {
			case <-ctx.Done():
				cont = false

			default:
			}

			if !cont {
				break
			}
		}

	default:
		return nil, errors.New("other operators should be handled already")
	}

	if len(tmpHeights) == 0 || firstRun {
		// Either:
		//
		// 1. Regardless if a previous match was attempted, which may have had
		// results, but no match was found for the current condition, then we
		// return no matches (assuming AND operand).
		//
		// 2. A previous match was not attempted, so we return all results.
		return tmpHeights, nil
	}

	// Remove/reduce matches in filteredHeights that were not found in this
	// match (tmpHeights).
	for k := range filteredHeights {
		cont := true

		if tmpHeights[k] == nil {
			delete(filteredHeights, k)

			select {
			case <-ctx.Done():
				cont = false

			default:
			}
		}

		if !cont {
			break
		}
	}

	return filteredHeights, nil
}

func (idx *BlockerIndexer) indexEvents(batch ds.Txn, events []abci.Event, typ string, height int64) error {
	heightBz := int64ToBytes(height)

	for _, event := range events {
		// only index events with a non-empty type
		if len(event.Type) == 0 {
			continue
		}

		for _, attr := range event.Attributes {
			if len(attr.Key) == 0 {
				continue
			}

			// index iff the event specified index:true and it's not a reserved event
			compositeKey := fmt.Sprintf("%s.%s", event.Type, string(attr.Key))
			if compositeKey == types.BlockHeightKey {
				return fmt.Errorf("event type and attribute key \"%s\" is reserved; please use a different key", compositeKey)
			}

			if attr.GetIndex() {
				key := eventKey(compositeKey, typ, attr.Value, height)

				if err := batch.Put(idx.ctx, ds.NewKey(key), heightBz); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

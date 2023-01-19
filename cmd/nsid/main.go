package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/celestiaorg/rollmint/store"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var (
	mainPrefix    = []byte{0}
	dalcPrefix    = []byte{1}
	indexerPrefix = []byte{2}
)

func main() {
	log.Println("NamespaceID recovery tool")

	homeFlag := flag.String("home", "", "home directory, like ~/.wordle")
	flag.Parse()

	if homeFlag == nil || len(*homeFlag) == 0 {
		log.Fatalf("Usage: %s -home <directory>\n", os.Args[0])
	}

	home := filepath.Join(*homeFlag, "data", "rollmint")
	fmt.Printf("reading data from '%s'", home)

	baseKV := store.NewDefaultKVStore(*homeFlag, "data", "rollmint")
	mainKV := store.NewPrefixKV(baseKV, mainPrefix)

	s := store.New(mainKV)

	block, err := s.LoadBlock(1)
	if err != nil {
		log.Fatalf("failed to get block 1: %v\n", err)
	}

	log.Println("NamespaceID:", strings.ToUpper(hex.EncodeToString(block.Header.NamespaceID[:])))
}

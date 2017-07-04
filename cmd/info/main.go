package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/go-audio/aiff"
)

var (
	flagPath = flag.String("path", "", "The path to the file to analyze")
)

func main() {
	flag.Parse()
	if *flagPath == "" {
		fmt.Println("You must set the -path flag")
		os.Exit(1)
	}
	f, err := os.Open(*flagPath)
	if err != nil {
		fmt.Println("Invalid path", *flagPath, err)
		os.Exit(1)
	}
	defer f.Close()

	d := aiff.NewDecoder(f)
	d.Drain()
	fmt.Println(d)
}

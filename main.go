package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	err := dbMigrate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	go func() {
		err := StartFetchIssues(ctx)
		if err != nil {
			panic(err)
		}
	}()
	go func() {
		err := StartDetermineMerged(ctx)
		if err != nil {
			panic(err)
		}
	}()
	go StartHTTPServer(5427)
	select {}
}

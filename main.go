package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pkg/errors"
)

func main() {
	err := Main()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}
}

func Main() error {
	err := InitGormDB()
	if err != nil {
		return err
	}

	err = dbMigrate()
	if err != nil {
		return err
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
	return errors.New("unreachable")
}

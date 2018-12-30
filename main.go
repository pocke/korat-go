package main

import "context"

func main() {
	ctx := context.Background()
	err := StartFetchIssues(ctx)
	if err != nil {
		panic(err)
	}
	select {}
}

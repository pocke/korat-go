package main

import (
	"context"
	"database/sql"
	"log"
	"time"
)

func StartDetermineMerged(ctx context.Context) error {
	as, err := SelectAccounts(ctx)
	if err != nil {
		return err
	}

	for _, a := range as {
		go func(a *AccountForGitHubAPI) {
			for {
				childCtx, cancel := context.WithCancel(ctx)
				err := startDetermineMerged(childCtx, a)
				log.Printf("%+v\n", err)
				err = sendErrToSlack(err)
				if err != nil {
					log.Printf("%+v\n", err)
				}
				cancel()
			}
		}(a)
	}
	return nil
}

func startDetermineMerged(ctx context.Context, account *AccountForGitHubAPI) error {
	client := ghClient(ctx, account.accessToken)

	for {
		time.Sleep(3 * time.Second)
		id, owner, name, number, err := SelectUndeterminedPullRequest(ctx, account.id)
		if err == sql.ErrNoRows {
			continue
		} else if err != nil {
			return err
		}
		pr, _, err := client.PullRequests.Get(ctx, owner, name, number)
		if err != nil {
			return err
		}
		merged := pr.GetMerged()

		DetermineMerged(ctx, id, merged)
	}
}

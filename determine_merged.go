package main

import (
	"context"
	"database/sql"
	"log"
	"time"
)

func StartDetermineMerged(ctx context.Context) error {
	accounts := make([]Account, 0)
	err := gormConn.Find(&accounts).Error
	if err != nil {
		return err
	}

	for _, a := range accounts {
		go func(a Account) {
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

func startDetermineMerged(ctx context.Context, account Account) error {
	client := ghClient(ctx, account.AccessToken)

	for {
		time.Sleep(3 * time.Second)
		id, owner, name, number, err := SelectUndeterminedPullRequest(ctx, account.ID)
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

		err = gormConn.Model(&Issue{ID: id}).Update("merged", merged).Error
		if err != nil {
			return err
		}
	}
}

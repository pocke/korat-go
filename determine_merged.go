package main

import (
	"context"
	"log"
	"time"

	"github.com/pkg/errors"
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
				err := errors.WithStack(startDetermineMerged(childCtx, a))
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
		i := Issue{}
		db := SelectUndeterminedPullRequest(account.ID).First(&i)
		if db.RecordNotFound() {
			continue
		} else if db.Error != nil {
			return db.Error
		}
		pr, _, err := client.PullRequests.Get(ctx, i.RepoOwner, i.RepoName, i.Number)
		if err != nil {
			return err
		}
		merged := pr.GetMerged()

		err = gormConn.Model(&Issue{ID: i.ID}).Update("merged", merged).Error
		if err != nil {
			return err
		}
	}
}

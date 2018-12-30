package main

import (
	"context"
	"time"

	"github.com/google/go-github/v21/github"
	_ "github.com/motemen/go-loghttp/global"

	"golang.org/x/oauth2"
)

func StartFetchIssues(ctx context.Context) error {
	chs, err := SelectChannels(ctx)
	if err != nil {
		return err
	}

	for _, c := range chs {
		client := ghClient(ctx, c.account.accessToken)
		for _, q := range c.queries {
			err := startFetchIssuesFor(ctx, client, c.id, q)
			if err != nil {
				return err
			}

		}
	}

	return nil
}

func startFetchIssuesFor(ctx context.Context, client *github.Client, channelID int, queryBase string) error {
	cnt, err := fetchAndSaveIssue(ctx, client, channelID, queryBase)
	if err != nil {
		return err
	}

	if cnt > 1 {
		go func() {
			err := fetchOldIssues(ctx, client, channelID, queryBase)
			if err != nil {
				panic(err)
			}
		}()
	}
	go func() {
		err := fetchNewIssues(ctx, client, channelID, queryBase)
		if err != nil {
			panic(err)
		}
	}()
	return nil
}

func fetchAndSaveIssue(ctx context.Context, client *github.Client, channelID int, query string) (int, error) {
	opt := &github.SearchOptions{
		Sort: "updated",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}
	deqSearchIssueQueue()
	issues, _, err := client.Search.Issues(ctx, query, opt)
	if err != nil {
		return -1, err
	}

	err = ImportIssues(ctx, issues.Issues, channelID)
	if err != nil {
		return -1, err
	}

	return len(issues.Issues), nil
}

func fetchOldIssues(ctx context.Context, client *github.Client, channelID int, queryBase string) error {
	oldestUpdatedAt, err := OldestIssueTime(ctx, channelID)
	if err != nil {
		return err
	}

	q := queryBase + " updated:<=" + fmtTime(oldestUpdatedAt)
	cnt, err := fetchAndSaveIssue(ctx, client, channelID, q)
	if err != nil {
		return err
	}
	if cnt > 1 {
		return fetchOldIssues(ctx, client, channelID, queryBase)
	}

	return nil
}

func fetchNewIssues(ctx context.Context, client *github.Client, channelID int, queryBase string) error {
	for {
		newestUpdatedAt, err := NewestIssueTime(ctx, channelID)
		if err != nil {
			return err
		}

		q := queryBase + " updated:>=" + fmtTime(newestUpdatedAt)
		_, err = fetchAndSaveIssue(ctx, client, channelID, q)
		if err != nil {
			return err
		}
	}
}

// TODO: Support GHE
func ghClient(ctx context.Context, accessToken string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: accessToken},
	)
	tc := oauth2.NewClient(ctx, ts)

	return github.NewClient(tc)
}

var searchIssueQueue = make(chan struct{}, 2)

// For rate limit
func deqSearchIssueQueue() {
	searchIssueQueue <- struct{}{}
	go func() {
		time.Sleep(5 * time.Second)
		<-searchIssueQueue
	}()
}

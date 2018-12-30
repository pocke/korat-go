package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-github/v21/github"
	_ "github.com/motemen/go-loghttp/global"

	"golang.org/x/oauth2"
)

func StartFetchIssues() error {
	chs, err := SelectChannels()
	if err != nil {
		return err
	}

	for _, c := range chs {
		client := ghClient(c.account.accessToken)
		for _, q := range c.queries {
			err := startFetchIssuesFor(client, c.id, q)
			if err != nil {
				return err
			}

		}
	}

	return nil
}

func startFetchIssuesFor(client *github.Client, channelID int, queryBase string) error {
	cnt, err := fetchAndSaveIssue(client, channelID, queryBase)
	if err != nil {
		return err
	}

	if cnt != 0 {
		// go fetchOldIssues(client, )
	}
	// go fetchNewIssues()
	return nil
}

func fetchAndSaveIssue(client *github.Client, channelID int, query string) (int, error) {
	ctx := context.Background()
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
	fmt.Println(len(issues.Issues))

	err = ImportIssues(issues.Issues, channelID)
	if err != nil {
		return -1, err
	}

	return len(issues.Issues), nil
}

// TODO: Support GHE
func ghClient(accessToken string) *github.Client {
	ctx := context.Background()
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

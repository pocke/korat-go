package main

import (
	"context"

	"github.com/google/go-github/v21/github"
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
			err := fetchAndSaveIssue(client, c.id, q)
			if err != nil {
				return err
			}

		}
	}

	return nil
}

func fetchAndSaveIssue(client *github.Client, channelID int, query string) error {
	ctx := context.Background()
	opt := &github.SearchOptions{
		Sort: "updated",
	}
	opt.ListOptions.PerPage = 100
	issues, _, err := client.Search.Issues(ctx, query, nil)
	if err != nil {
		return err
	}

	err = ImportIssues(issues.Issues, channelID)
	if err != nil {
		return err
	}

	return nil
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

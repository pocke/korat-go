package main

import (
	"context"
	"database/sql"
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
	cnt, err := fetchAndSaveIssue(ctx, client, channelID, &fetchIssueQuery{base: queryBase})
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

type fetchIssueQuery struct {
	base string
	cond string
}

func (q *fetchIssueQuery) build() string {
	if q.cond == "" {
		return q.base
	} else {
		return q.base + " " + q.cond
	}
}

func fetchAndSaveIssue(ctx context.Context, client *github.Client, channelID int, query *fetchIssueQuery) (int, error) {
	opt := &github.SearchOptions{
		Sort: "updated",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}
	deqSearchIssueQueue()
	issues, _, err := client.Search.Issues(ctx, query.build(), opt)
	if err != nil {
		return -1, err
	}

	err = ImportIssues(ctx, issues.Issues, channelID, query.base)
	if err != nil {
		return -1, err
	}

	if err := notifyUnreadCount(ctx, issues.Issues); err != nil {
		return 0, err
	}

	return len(issues.Issues), nil
}

func fetchOldIssues(ctx context.Context, client *github.Client, channelID int, queryBase string) error {
	var qid int
	err := tx(func(tx *sql.Tx) error {
		var err error
		qid, err = findOrCreateQuery(ctx, queryBase, Conn)
		return err
	})
	if err != nil {
		return err
	}

	for {
		oldestUpdatedAt, err := OldestIssueTime(ctx, qid)
		if err != nil {
			return err
		}

		q := &fetchIssueQuery{base: queryBase, cond: "updated:<=" + fmtTime(oldestUpdatedAt)}
		cnt, err := fetchAndSaveIssue(ctx, client, channelID, q)
		if err != nil {
			return err
		}
		if cnt <= 1 {
			break
		}
	}

	return nil
}

func fetchNewIssues(ctx context.Context, client *github.Client, channelID int, queryBase string) error {
	var qid int
	err := tx(func(tx *sql.Tx) error {
		var err error
		qid, err = findOrCreateQuery(ctx, queryBase, Conn)
		return err
	})
	if err != nil {
		return err
	}

	for {
		newestUpdatedAt, err := NewestIssueTime(ctx, qid)
		if err != nil {
			return err
		}

		q := &fetchIssueQuery{base: queryBase, cond: "updated:>=" + fmtTime(newestUpdatedAt)}
		_, err = fetchAndSaveIssue(ctx, client, channelID, q)
		if err != nil {
			return err
		}
	}
}

func notifyUnreadCount(ctx context.Context, issues []github.Issue) error {
	ids := make([]int, len(issues))
	for idx, i := range issues {
		ids[idx] = (int)(i.GetID())
	}

	cnts, err := UnreadCountForIssue(ctx, ids)
	if err != nil {
		return err
	}

	for _, cnt := range cnts {
		unreadCountNotifier.Notify(cnt)
	}
	return nil
}

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

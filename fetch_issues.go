package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/google/go-github/v21/github"
	"github.com/jinzhu/gorm"
	_ "github.com/motemen/go-loghttp/global"
	"github.com/pkg/errors"

	"golang.org/x/oauth2"
)

func StartFetchIssues(ctx context.Context) error {
	chs := make([]Channel, 0)
	err := gormConn.Preload("Account").Find(&chs).Error
	if err != nil {
		return err
	}

	qs, err := BuildActualQuery(ctx, chs)
	if err != nil {
		return err
	}

	for _, q := range qs {
		go func(q ActualQuery) {
			for {
				childCtx, cancel := context.WithCancel(ctx)
				err := errors.WithStack(startFetchIssuesWithChannel(childCtx, q))
				log.Printf("%+v\n", err)
				err = sendErrToSlack(err)
				if err != nil {
					log.Printf("%+v\n", err)
				}
				cancel()
				time.Sleep(1 * time.Second)
			}
		}(q)
	}

	return nil
}

type SlackMsg struct {
	Text string `json:"text"`
}

func sendErrToSlack(err error) error {
	addr := os.Getenv("KORAT_ERRORS_SLACK_HOOK_URL")
	if addr == "" {
		return errors.New("KORAT_ERRORS_SLACK_HOOK_URL is empty.")
	}
	params, err := json.Marshal(SlackMsg{fmt.Sprintf("```\n%+v\n```", err)})
	if err != nil {
		return err
	}
	_, err = http.PostForm(addr, url.Values{"payload": {string(params)}})
	return err
}

func startFetchIssuesWithChannel(ctx context.Context, q ActualQuery) error {
	errCh := make(chan error)
	err := startFetchIssuesFor(ctx, q, errCh)
	if err != nil {
		return err
	}
	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return errors.New("Unreachable")
}

func startFetchIssuesFor(ctx context.Context, q ActualQuery, errCh chan<- error) error {
	client := ghClient(ctx, q.accessToken)
	go func() {
		errCh <- fetchOldIssues(ctx, client, q)
	}()
	go func() {
		errCh <- fetchNewIssues(ctx, client, q)
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

func fetchAndSaveIssue(ctx context.Context, client *github.Client, q ActualQuery, query *fetchIssueQuery, order string) (int, error) {
	opt := &github.SearchOptions{
		Sort:  "updated",
		Order: order,
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}
	deqSearchIssueQueue()
	issues, _, err := client.Search.Issues(ctx, query.build(), opt)
	if err != nil {
		return -1, err
	}

	cidMap := make(map[int][]github.Issue)
	for _, i := range issues.Issues {
		for _, cond := range q.conditions {
			if cond.satisfy(i) {
				cidMap[cond.channel.ID] = append(cidMap[cond.channel.ID], i)
			}
		}
	}

	for cid, is := range cidMap {
		err := ImportIssues(ctx, is, cid, query.base)
		if err != nil {
			return -1, err
		}
	}

	if err := notifyUnreadCount(ctx, issues.Issues); err != nil {
		return 0, err
	}

	return len(issues.Issues), nil
}

func fetchOldIssues(ctx context.Context, client *github.Client, q ActualQuery) error {
	var qid int
	err := txGorm(func(tx *gorm.DB) error {
		q := Query{Query: q.query}
		err := tx.FirstOrCreate(&q, q).Error
		qid = q.ID
		return err
	})
	if err != nil {
		return err
	}

	for {
		var oldestUpdatedAt time.Time
		i := Issue{}
		res := EdgeIssueTime(qid, "asc").First(&i)
		if res.RecordNotFound() {
			oldestUpdatedAt = time.Now().UTC()
		} else if res.Error != nil {
			return res.Error
		} else {
			oldestUpdatedAt, err = parseTime(i.UpdatedAt)
			if err != nil {
				return err
			}
		}

		// Ignore too old issues
		if oldestUpdatedAt.After(time.Now().Add(1 * 365 * 24 * time.Hour)) {
			break
		}

		fq := &fetchIssueQuery{base: q.query, cond: "updated:<=" + fmtTime(oldestUpdatedAt)}
		cnt, err := fetchAndSaveIssue(ctx, client, q, fq, "desc")
		if err != nil {
			return err
		}
		if cnt <= 1 {
			break
		}
	}

	return nil
}

func fetchNewIssues(ctx context.Context, client *github.Client, q ActualQuery) error {
	var qid int
	err := txGorm(func(tx *gorm.DB) error {
		q := Query{Query: q.query}
		err := tx.FirstOrCreate(&q, q).Error
		qid = q.ID
		return err
	})
	if err != nil {
		return err
	}

	for {
		var newestUpdatedAt time.Time
		i := Issue{}
		res := EdgeIssueTime(qid, "desc").First(&i)
		if res.RecordNotFound() {
			newestUpdatedAt = time.Now().UTC()
		} else if res.Error != nil {
			return res.Error
		} else {
			newestUpdatedAt, err = parseTime(i.UpdatedAt)
			if err != nil {
				return err
			}
		}

		fq := &fetchIssueQuery{base: q.query, cond: "updated:>=" + fmtTime(newestUpdatedAt)}
		_, err = fetchAndSaveIssue(ctx, client, q, fq, "asc")
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

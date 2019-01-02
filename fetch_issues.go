package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/v21/github"
	_ "github.com/motemen/go-loghttp/global"
	"github.com/pkg/errors"

	"golang.org/x/oauth2"
)

// GitHub does not accept too large URI,
// so korat should separate requests if query is too long.
// The limit is actually about 6800. This limit is investigated with binary searching with GitHub API.
// This constant has leeway.
const GitHubURIlimit = 5000

func StartFetchIssues(ctx context.Context) error {
	chs, err := SelectChannels(ctx)
	if err != nil {
		return err
	}

	for _, c := range chs {
		go func(c Channel) {
			for {
				childCtx, cancel := context.WithCancel(ctx)
				err := startFetchIssuesWithChannel(childCtx, c)
				log.Printf("%+v\n", err)
				err = sendErrToSlack(err)
				if err != nil {
					log.Printf("%+v\n", err)
				}
				cancel()
			}
		}(c)
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

func startFetchIssuesWithChannel(ctx context.Context, c Channel) error {
	client := ghClient(ctx, c.account.accessToken)
	var qs []string
	if c.system.Valid == true {
		var err error
		qs, err = buildSystemQueries(ctx, c.system.String, client)
		if err != nil {
			return err
		}
	} else {
		qs = c.queries
	}

	errCh := make(chan error)
	for _, q := range qs {
		err := startFetchIssuesFor(ctx, client, c.id, q, errCh)
		if err != nil {
			return err
		}
	}
	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return errors.New("Unreachable")
}

func startFetchIssuesFor(ctx context.Context, client *github.Client, channelID int, queryBase string, errCh chan<- error) error {
	cnt, err := fetchAndSaveIssue(ctx, client, channelID, &fetchIssueQuery{base: queryBase})
	if err != nil {
		return err
	}

	if cnt > 1 {
		go func() {
			errCh <- fetchOldIssues(ctx, client, channelID, queryBase)
		}()
	}
	go func() {
		errCh <- fetchNewIssues(ctx, client, channelID, queryBase)
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

func buildSystemQueries(ctx context.Context, kind string, client *github.Client) ([]string, error) {
	switch kind {
	case "teams":
		var allTeams []*github.Team
		opt := &github.ListOptions{PerPage: 100}
		for {
			teams, resp, err := client.Teams.ListUserTeams(ctx, opt)
			if err != nil {
				return nil, err
			}
			allTeams = append(allTeams, teams...)
			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage
		}
		var q []string
		for _, t := range allTeams {
			q = append(q, fmt.Sprintf("team:%s/%s", t.Organization.GetLogin(), t.GetSlug()))
		}

		return []string{strings.Join(q, " ")}, nil
	case "watching":
		var allRepos []*github.Repository
		opt := &github.ListOptions{PerPage: 100}
		for {
			repos, resp, err := client.Activity.ListWatched(ctx, "", opt)
			if err != nil {
				return nil, err
			}
			allRepos = append(allRepos, repos...)
			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage
		}
		res := []string{""}
		for _, r := range allRepos {
			q := fmt.Sprintf("repo:%s", r.GetFullName())
			lastIdx := len(res) - 1
			last := res[lastIdx]
			newQuery := last + " " + q
			if len(newQuery) < GitHubURIlimit {
				res[lastIdx] = newQuery
			} else {
				res = append(res, q)
			}
		}
		return res, nil
	default:
		return nil, errors.Errorf("%s is not a valid system type.", kind)
	}
}

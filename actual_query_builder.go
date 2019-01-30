package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/google/go-github/v21/github"
	"github.com/pkg/errors"
)

// GitHub does not accept too large URI,
// so korat should separate requests if query is too long.
// The limit is actually about 6800. This limit is investigated with binary searching with GitHub API.
// This constant has leeway.
const GitHubURIlimit = 5000

type ActualQuery struct {
	query       string
	conditions  []Condition
	accessToken string
}

type Condition struct {
	unlessRepository []struct {
		Owner string
		Name  string
	}
	channel Channel
}

func (c *Condition) satisfy(i github.Issue) bool {
	owner, name := repoInfoFromIssue(i)
	for _, r := range c.unlessRepository {
		if owner == r.Owner && name == r.Name {
			return false
		}
	}
	return true
}

func BuildActualQuery(ctx context.Context, cs []Channel) ([]ActualQuery, error) {
	log.Println("Start to build actual queries")
	res := make([]ActualQuery, 0)
	for _, c := range cs {
		token := c.Account.AccessToken
		qs, err := c.Queries(ctx)
		if err != nil {
			return nil, err
		}

		for _, q := range qs {
			if isOptimizableQuery(q) {

			} else {
				aq := ActualQuery{
					query:       q,
					conditions:  []Condition{{channel: c}},
					accessToken: token,
				}
				res = append(res, aq)
			}
		}
	}
	log.Printf("Build %d queries", len(res))
	return res, nil
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

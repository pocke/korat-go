package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"time"

	"github.com/google/go-github/v21/github"
)

type Account struct {
	id          int
	displayName string
	urlBase     string
	apiUrlBase  string
	accessToken string
}

type Channel struct {
	id          int
	displayName string
	system      sql.NullString
	queries     []string

	account *Account
}

func SelectChannels(ctx context.Context) ([]Channel, error) {
	res := make([]Channel, 0)
	accounts := make(map[int]*Account)

	rows, err := Conn.QueryContext(ctx, `
		select
			c.id, c.displayName, c.system, c.queries, a.id
		from
			channels as c,
			accounts as a
		where
			c.accountID = a.id;
	`)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var accountID int
		var ch Channel
		var queries string

		if err := rows.Scan(&ch.id, &ch.displayName, &ch.system, &queries, &accountID); err != nil {
			return nil, err
		}
		err := json.Unmarshal([]byte(queries), &ch.queries)
		if err != nil {
			return nil, err
		}

		if a, ok := accounts[accountID]; ok {
			ch.account = a
		} else {
			a := &Account{}
			err := Conn.QueryRowContext(ctx, `
				select
					id, displayName, urlBase, apiUrlBase, accessToken
				from
					accounts
				where
					id = ?
			`, accountID).Scan(&a.id, &a.displayName, &a.urlBase, &a.apiUrlBase, &a.accessToken)
			if err != nil {
				return nil, err
			}
			ch.account = a
			accounts[accountID] = a
		}
		res = append(res, ch)
	}

	return res, nil
}

var RepoFromIssueUrlRe = regexp.MustCompile(`/([^/]+)/([^/]+)/issues/\d+$`)

func ImportIssues(ctx context.Context, issues []github.Issue, channelID int) error {
	return tx(func(tx *sql.Tx) error {
		for _, i := range issues {
			url := i.GetURL()
			m := RepoFromIssueUrlRe.FindStringSubmatch(url)
			repoOwner := m[1]
			repoName := m[2]

			user := i.GetUser()
			userID := user.GetID()
			_, err := tx.ExecContext(ctx, `
				replace into github_users
				(id, login, avatarURL)
				values (?, ?, ?)
			`, userID, user.GetLogin(), user.GetAvatarURL())

			id := i.GetID()
			exist, err := rowExist(ctx, "issues", (int)(id), tx)
			if err != nil {
				return err
			}

			createdAt := fmtTime(i.GetCreatedAt())
			updatedAt := fmtTime(i.GetUpdatedAt())
			var closedAt sql.NullString
			if i.ClosedAt == nil {
				closedAt.Valid = false
			} else {
				closedAt.Valid = true
				closedAt.String = fmtTime(*i.ClosedAt)
			}

			if exist {
				_, err = tx.ExecContext(ctx, `
					update issues
					set number = ?, title = ?, userID = ?, repoOwner = ?, repoName = ?, state = ?, locked = ?, comments = ?, createdAt = ?, updatedAt = ?, closedAt = ?, isPullRequest = ?, body = ?
					where id = ?
				`, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(), createdAt, updatedAt, closedAt, i.IsPullRequest(), i.GetBody(), id)
				if err != nil {
					return err
				}
			} else {
				_, err = tx.ExecContext(ctx, `
					insert into issues
					(id, number, title, userID, repoOwner, repoName, state, locked, comments, createdAt, updatedAt, closedAt, isPullRequest, body, alreadyRead)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, id, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(), createdAt, updatedAt, closedAt, i.IsPullRequest(), i.GetBody(), false)
				if err != nil {
					return err
				}
			}

			_, err = tx.ExecContext(ctx, `
				replace into channel_issues
				(issueID, channelID)
				values (?, ?)
			`, id, channelID)
			if err != nil {
				return err
			}

		}
		return nil
	})
}

func fmtTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

func OldestIssueTime(ctx context.Context, channelID int) (time.Time, error) {
	var t string
	err := Conn.QueryRowContext(ctx, `
		select
			i.updatedAt
		from
			issues as i,
			channel_issues as ci
		where
			i.id = ci.issueID AND
			ci.channelID = ?
		order by i.updatedAt
		limit 1
		;
`, channelID).Scan(&t)
	if err != nil {
		return time.Time{}, err
	}

	return parseTime(t)
}

func NewestIssueTime(ctx context.Context, channelID int) (time.Time, error) {
	var t string
	err := Conn.QueryRowContext(ctx, `
		select
			i.updatedAt
		from
			issues as i,
			channel_issues as ci
		where
			i.id = ci.issueID AND
			ci.channelID = ?
		order by i.updatedAt desc
		limit 1
		;
`, channelID).Scan(&t)
	if err != nil {
		return time.Time{}, err
	}

	return parseTime(t)
}

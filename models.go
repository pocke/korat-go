package main

import (
	"database/sql"
	"encoding/json"
	"regexp"

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

func SelectChannels() ([]Channel, error) {
	res := make([]Channel, 0)
	accounts := make(map[int]*Account)

	rows, err := Conn.Query(`
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
			err := Conn.QueryRow(`
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

func ImportIssues(issues []github.Issue, channelID int) error {
	return tx(func(tx *sql.Tx) error {
		for _, i := range issues {
			url := i.GetURL()
			m := RepoFromIssueUrlRe.FindStringSubmatch(url)
			repoOwner := m[1]
			repoName := m[2]

			user := i.GetUser()
			userID := user.GetID()
			_, err := tx.Exec(`
				replace into github_users
				(id, login, avatarURL)
				values (?, ?, ?)
			`, userID, user.GetLogin(), user.GetAvatarURL())

			id := i.GetID()
			exist, err := rowExist("issues", (int)(id), tx)
			if err != nil {
				return err
			}

			if exist {
				_, err = tx.Exec(`
					update issues
					set number = ?, title = ?, userID = ?, repoOwner = ?, repoName = ?, state = ?, locked = ?, comments = ?, createdAt = ?, updatedAt = ?, closedAt = ?, isPullRequest = ?, body = ?
					where id = ?
				`, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(), i.GetCreatedAt(), i.GetUpdatedAt(), i.GetClosedAt(), i.IsPullRequest(), i.GetBody(), id)
				if err != nil {
					return err
				}
			} else {
				_, err = tx.Exec(`
					insert into issues
					(id, number, title, userID, repoOwner, repoName, state, locked, comments, createdAt, updatedAt, closedAt, isPullRequest, body, alreadyRead)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, id, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(), i.GetCreatedAt(), i.GetUpdatedAt(), i.GetClosedAt(), i.IsPullRequest(), i.GetBody(), false)
				if err != nil {
					return err
				}
			}

			_, err = tx.Exec(`
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

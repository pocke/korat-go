package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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
			c.id, c.displayName, c.system, c.queries, c.accountID
		from
			channels as c
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

func SelectAccountForAPI(ctx context.Context) ([]*ResponseAccount, error) {
	res := make([]*ResponseAccount, 0)
	rows, err := Conn.QueryContext(ctx, `
		select
			id, displayName, urlBase, apiUrlBase
		from
			accounts
		;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		a := &ResponseAccount{}
		err := rows.Scan(&a.ID, &a.DisplayName, &a.UrlBase, &a.ApiUrlBase)
		if err != nil {
			return nil, err
		}
		channelRows, err := Conn.QueryContext(ctx, `
			select
					ID, DisplayName, System, Queries
			from
					channels
			where
					accountID = ?
			;
		`, a.ID)
		if err != nil {
			return nil, err
		}
		for channelRows.Next() {
			c := &ResponseChannel{}
			var queries string
			err := channelRows.Scan(&c.ID, &c.DisplayName, &c.System, &queries)
			if err != nil {
				channelRows.Close()
				return nil, err
			}
			err = json.Unmarshal([]byte(queries), &c.Queries)
			if err != nil {
				channelRows.Close()
				return nil, err
			}

			a.Channels = append(a.Channels, c)
		}
		channelRows.Close()

		res = append(res, a)
	}

	return res, nil
}

type Issue struct {
	ID            int
	Number        int
	Title         string
	UserID        int
	RepoOwner     string
	RepoName      string
	State         string
	Locked        bool
	Comments      int
	CreatedAt     string
	UpdatedAt     string
	ClosedAt      sql.NullString
	IsPullRequest bool
	Body          string
	AlreadyRead   bool

	Labels []*Label
}

type Label struct {
	ID      int
	Name    string
	Color   string
	Default bool
}

func SelectIssues(ctx context.Context, channelID, page, perPage int) ([]*Issue, error) {
	res := make([]*Issue, 0)
	rows, err := Conn.QueryContext(ctx, `
		select
			i.id, i.number, i.title, i.userID, i.repoOwner, i.repoName, i.state, i.locked, i.comments, i.createdAt, i.updatedAt, i.closedAt, i.isPullREquest, i.body, i.alreadyRead
		from
			issues as i,
			channel_issues as ci
		where
			i.id = ci.issueID AND
			ci.channelID = ?
		order by
			i.updatedAt desc
		limit
			?
		offset
			?
		;
	`, channelID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		i := &Issue{
			Labels: []*Label{},
		}
		err := rows.Scan(&i.ID, &i.Number, &i.Title, &i.UserID, &i.RepoOwner, &i.RepoName, &i.State, &i.Locked, &i.Comments, &i.CreatedAt, &i.UpdatedAt, &i.ClosedAt, &i.IsPullRequest, &i.Body, &i.AlreadyRead)
		if err != nil {
			return nil, err
		}
		res = append(res, i)
	}

	if err := includeLabelsToIssues(ctx, res); err != nil {
		return nil, err
	}

	return res, nil
}

func includeLabelsToIssues(ctx context.Context, issues []*Issue) error {
	issueIDs := make([]string, len(issues))
	issueMap := make(map[int]*Issue, len(issues))
	for idx, i := range issues {
		issueIDs[idx] = strconv.Itoa(i.ID)
		issueMap[i.ID] = i
	}

	rows, err := Conn.QueryContext(ctx, fmt.Sprintf(`
		select
			l.id, l.name, l.color, l.'default', li.issueID
		from
			labels as l,
			assigned_labels_to_issue as li
		where
			l.id = li.labelID AND
			li.issueID IN (%s)
		;
	`, strings.Join(issueIDs, ", ")))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		label := &Label{}
		var issueID int

		if err := rows.Scan(&label.ID, &label.Name, &label.Color, &label.Default, &issueID); err != nil {
			return err
		}
		issueMap[issueID].Labels = append(issueMap[issueID].Labels, label)
	}

	return nil
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
			if err != nil {
				return err
			}

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

			var milestoneID sql.NullInt64
			var milestone = i.GetMilestone()
			if milestone == nil {
				milestoneID.Valid = false
			} else {
				milestoneID.Valid = true
				milestoneID.Int64 = milestone.GetID()

				if err := insertMilestone(ctx, milestone, tx); err != nil {
					return err
				}
			}

			if exist {
				_, err = tx.ExecContext(ctx, `
					update issues
					set number = ?, title = ?, userID = ?, repoOwner = ?, repoName = ?, state = ?, locked = ?, comments = ?, createdAt = ?, updatedAt = ?, closedAt = ?, isPullRequest = ?, body = ?, milestoneID = ?
					where id = ?
				`, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(), createdAt, updatedAt, closedAt, i.IsPullRequest(), i.GetBody(), milestoneID, id)
				if err != nil {
					return err
				}
			} else {
				_, err = tx.ExecContext(ctx, `
					insert into issues
					(id, number, title, userID, repoOwner, repoName, state, locked, comments, createdAt, updatedAt, closedAt, isPullRequest, body, alreadyRead, milestoneID)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, id, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(), createdAt, updatedAt, closedAt, i.IsPullRequest(), i.GetBody(), false, milestoneID)
				if err != nil {
					return err
				}
			}

			if err := importLabels(ctx, i, tx); err != nil {
				return err
			}
			if err := importAssignees(ctx, i, tx); err != nil {
				return err
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

func importLabels(ctx context.Context, issue github.Issue, tx *sql.Tx) error {
	issueID := issue.GetID()
	_, err := tx.ExecContext(ctx, `
			delete from assigned_labels_to_issue
			where issueID = ?
		`, issueID)
	if err != nil {
		return err
	}

	for _, label := range issue.Labels {
		labelID := label.GetID()

		_, err := tx.ExecContext(ctx, `
				replace into labels
				(id, name, color, 'default')
				VALUES (?, ?, ?, ?)
			`, labelID, label.GetName(), label.GetColor(), label.GetDefault())
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, `
				insert into assigned_labels_to_issue
				(issueID, labelID)
				VALUES (?, ?)
			`, issueID, labelID)
		if err != nil {
			return err
		}
	}

	return nil
}

func insertMilestone(ctx context.Context, milestone *github.Milestone, tx *sql.Tx) error {
	mID := milestone.GetID()
	createdAt := fmtTime(milestone.GetCreatedAt())
	updatedAt := fmtTime(milestone.GetUpdatedAt())
	var closedAt sql.NullString
	if milestone.ClosedAt == nil {
		closedAt.Valid = false
	} else {
		closedAt.Valid = true
		closedAt.String = fmtTime(*milestone.ClosedAt)
	}

	_, err := tx.ExecContext(ctx, `
			replace into milestones
			(id, number, title, description, state, createdAt, updatedAt, closedAt)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, mID, milestone.GetNumber(), milestone.GetTitle(), milestone.GetDescription(), milestone.GetState(), createdAt, updatedAt, closedAt)
	if err != nil {
		return err
	}

	return nil
}

func importAssignees(ctx context.Context, issue github.Issue, tx *sql.Tx) error {
	issueID := issue.GetID()
	_, err := tx.ExecContext(ctx, `
			delete from assigned_users_to_issue
			where issueID = ?
		`, issueID)
	if err != nil {
		return err
	}

	for _, user := range issue.Assignees {
		userID := user.GetID()

		_, err := tx.ExecContext(ctx, `
				replace into github_users
				(id, login, avatarURL)
				VALUES (?, ?, ?)
			`, userID, user.GetLogin(), user.GetAvatarURL())
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, `
				insert into assigned_users_to_issue
				(issueID, userID)
				VALUES (?, ?)
			`, issueID, userID)
		if err != nil {
			return err
		}
	}

	return nil
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

func UpdateIssueAlreadyRead(ctx context.Context, issueID int, alreadyRead bool) error {
	_, err := Conn.ExecContext(ctx, `
		update issues
		set alreadyRead = ?
		where id = ?
	`, alreadyRead, issueID)
	return err
}

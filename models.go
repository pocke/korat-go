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

type AccountOld struct {
	id          int
	displayName string
	urlBase     string
	apiUrlBase  string
	accessToken string
}

type ChannelOld struct {
	id          int
	displayName string
	system      sql.NullString
	queries     []string

	account *AccountOld
}

func SelectChannels(ctx context.Context) ([]ChannelOld, error) {
	res := make([]ChannelOld, 0)
	accounts := make(map[int]*AccountOld)

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
		var ch ChannelOld
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
			a := &AccountOld{}
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

func SelectChannelsUnreadCount(ctx context.Context) ([]*UnreadCount, error) {
	res := make([]*UnreadCount, 0)

	rows, err := Conn.QueryContext(ctx, `
		select
			X.channelID, count(X.issueID)
		from
			(
				select distinct
					channelID, issueID
				from
					channel_issues as ci,
					issues as i
				where
					ci.issueID = i.id AND
					i.alreadyRead = 0
			) as X
		group by
			X.channelID
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		c := &UnreadCount{}
		if err := rows.Scan(&c.ChannelID, &c.Count); err != nil {
			return nil, err
		}
		res = append(res, c)
	}

	return res, nil
}

func UnreadCountForIssue(ctx context.Context, issueIDs []int) ([]*UnreadCount, error) {
	issueIDsStr := make([]string, len(issueIDs))
	for idx, issueID := range issueIDs {
		issueIDsStr[idx] = strconv.Itoa(issueID)
	}

	rows, err := Conn.QueryContext(ctx, fmt.Sprintf(`
		select distinct
			channelID
		from
			channel_issues
		where
			issueID IN (%s)
	`, strings.Join(issueIDsStr, ",")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	channelIDs := make([]string, 0)
	res := make([]*UnreadCount, 0)
	channelMap := make(map[int]*UnreadCount, 0)

	for i := 0; rows.Next(); i++ {
		c := &UnreadCount{Count: 0}
		err := rows.Scan(&c.ChannelID)
		if err != nil {
			return nil, err
		}
		res = append(res, c)
		channelIDs = append(channelIDs, strconv.Itoa(c.ChannelID))
		channelMap[c.ChannelID] = c
	}

	rows, err = Conn.QueryContext(ctx, fmt.Sprintf(`
		select
			X.channelID, count(X.issueID)
		from
			(
				select distinct
					channelID, issueID
				from
					channel_issues as ci,
					issues as i
				where
					ci.issueID = i.id AND
					ci.channelID IN (%s) AND
					i.alreadyRead = 0
			) as X
		group by
			X.channelID
	`, strings.Join(channelIDs, ",")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var channelID int
		var cnt int
		err := rows.Scan(&channelID, &cnt)
		if err != nil {
			return nil, err
		}

		channelMap[channelID].Count = cnt
	}

	return res, nil
}

type NullStringJSON struct {
	sql.NullString
}

func (n NullStringJSON) MarshalJSON() ([]byte, error) {
	if n.Valid {
		return json.Marshal(n.String)
	}
	return []byte("null"), nil
}

type NullBoolJSON struct {
	sql.NullBool
}

func (n NullBoolJSON) MarshalJSON() ([]byte, error) {
	if n.Valid {
		return json.Marshal(n.Bool)
	}
	return []byte("null"), nil
}

type IssueOld struct {
	ID            int
	Number        int
	Title         string
	RepoOwner     string
	RepoName      string
	State         string
	Locked        bool
	Comments      int
	CreatedAt     string
	UpdatedAt     string
	ClosedAt      NullStringJSON
	IsPullRequest bool
	Body          string
	AlreadyRead   bool
	Merged        NullBoolJSON

	UserOld   *UserOld
	Labels    []*LabelOld
	Assignees []*UserOld
}

type LabelOld struct {
	ID      int
	Name    string
	Color   string
	Default bool
}

type UserOld struct {
	ID        int
	Login     string
	AvatarURL string
}

func SelectIssues(ctx context.Context, q *SearchIssuesQuery) ([]*IssueOld, error) {
	res := make([]*IssueOld, 0)
	additionalConds := buildFilterForSelectIssues(q.filter)

	rows, err := Conn.QueryContext(ctx, fmt.Sprintf(`
		select distinct
			i.id, i.number, i.title, i.repoOwner, i.repoName, i.state, i.locked, i.comments, i.createdAt, i.updatedAt, i.closedAt, i.isPullREquest, i.body, i.alreadyRead, i.merged,
			u.id, u.login, u.avatarURL
		from
			issues as i,
			channel_issues as ci,
			github_users as u
		where
			i.id = ci.issueID AND
			u.id = i.userID AND
			ci.channelID = ?
			%s
		order by
			i.updatedAt desc
		limit
			?
		offset
			?
		;
	`, additionalConds), q.channelID, q.perPage, (q.page-1)*q.perPage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		u := &UserOld{}
		i := &IssueOld{
			Labels:    []*LabelOld{},
			Assignees: []*UserOld{},
			UserOld:   u,
		}
		err := rows.Scan(&i.ID, &i.Number, &i.Title, &i.RepoOwner, &i.RepoName, &i.State, &i.Locked, &i.Comments, &i.CreatedAt, &i.UpdatedAt, &i.ClosedAt, &i.IsPullRequest, &i.Body, &i.AlreadyRead, &i.Merged,
			&u.ID, &u.Login, &u.AvatarURL)
		if err != nil {
			return nil, err
		}
		res = append(res, i)
	}

	if err := includeLabelsToIssues(ctx, res); err != nil {
		return nil, err
	}
	if err := includeAssigneesToIssues(ctx, res); err != nil {
		return nil, err
	}

	return res, nil
}

func buildFilterForSelectIssues(f *SearchIssueFilter) string {
	res := ""
	if f.Issue && !f.PullRequest {
		res += " AND i.isPullRequest = 0 "
	}
	if !f.Issue && f.PullRequest {
		res += " AND i.isPullRequest = 1 "
	}

	if f.Read && !f.Unread {
		res += " AND i.alreadyRead = 1 "
	}
	if !f.Read && f.Unread {
		res += " AND i.alreadyRead = 0 "
	}

	if f.Closed && f.Open && f.Merged {
		return res
	}

	s := []string{}

	if f.Closed {
		s = append(s, `(i.isPullRequest = 0 AND i.closedAt is not null)`, `(i.isPullRequest = 1 AND i.merged = 0)`)
	}
	if f.Open {
		s = append(s, `(i.closedAt is null)`)
	}
	if f.Merged {
		s = append(s, `(i.merged = 1)`)
	}

	res += fmt.Sprintf(" AND (%s)", strings.Join(s, " OR "))

	return res
}

func includeLabelsToIssues(ctx context.Context, issues []*IssueOld) error {
	issueIDs := make([]string, len(issues))
	issueMap := make(map[int]*IssueOld, len(issues))
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
		label := &LabelOld{}
		var issueID int

		if err := rows.Scan(&label.ID, &label.Name, &label.Color, &label.Default, &issueID); err != nil {
			return err
		}
		issueMap[issueID].Labels = append(issueMap[issueID].Labels, label)
	}

	return nil
}

func includeAssigneesToIssues(ctx context.Context, issues []*IssueOld) error {
	issueIDs := make([]string, len(issues))
	issueMap := make(map[int]*IssueOld, len(issues))
	for idx, i := range issues {
		issueIDs[idx] = strconv.Itoa(i.ID)
		issueMap[i.ID] = i
	}

	rows, err := Conn.QueryContext(ctx, fmt.Sprintf(`
		select
			u.id, u.login, u.avatarURL, ui.issueID
		from
			github_users as u,
			assigned_users_to_issue as ui
		where
			u.id = ui.userID AND
			ui.issueID IN (%s)
		;
	`, strings.Join(issueIDs, ", ")))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		user := &UserOld{}
		var issueID int

		if err := rows.Scan(&user.ID, &user.Login, &user.AvatarURL, &issueID); err != nil {
			return err
		}
		issueMap[issueID].Assignees = append(issueMap[issueID].Assignees, user)
	}

	return nil
}

var RepoFromIssueUrlRe = regexp.MustCompile(`/([^/]+)/([^/]+)/issues/\d+$`)

func ImportIssues(ctx context.Context, issues []github.Issue, channelID int, query string) error {
	return tx(func(tx *sql.Tx) error {
		qid, err := findOrCreateQuery(ctx, query, tx)
		if err != nil {
			return err
		}

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
			prevUpdatedAt, prevAlreadyRead, err := issueUpdatedAtAndAlreadyRead(ctx, (int)(id), tx)
			var exist bool
			if err == sql.ErrNoRows {
				exist = false
			} else if err != nil {
				return err
			} else {
				exist = true
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
					set number = ?, title = ?, userID = ?, repoOwner = ?, repoName = ?, state = ?, locked = ?, comments = ?,
					createdAt = ?, updatedAt = ?, closedAt = ?, isPullRequest = ?, body = ?, milestoneID = ?, alreadyRead = ?
					where id = ?
				`, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(),
					createdAt, updatedAt, closedAt, i.IsPullRequest(), i.GetBody(), milestoneID, prevAlreadyRead && prevUpdatedAt.Equal(i.GetUpdatedAt()), id)
				if err != nil {
					return err
				}
			} else {
				// If issue is too old, it is marked as read
				alreadyRead := i.GetUpdatedAt().Before(time.Now().Add(-24 * 30 * time.Hour))
				_, err = tx.ExecContext(ctx, `
					insert into issues
					(id, number, title, userID, repoOwner, repoName, state, locked, comments,
					createdAt, updatedAt, closedAt, isPullRequest, body, alreadyRead, milestoneID)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, id, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(),
					createdAt, updatedAt, closedAt, i.IsPullRequest(), i.GetBody(), alreadyRead, milestoneID)
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
				(issueID, channelID, queryID)
				values (?, ?, ?)
			`, id, channelID, qid)
			if err != nil {
				return err
			}

		}
		return nil
	})
}

func issueUpdatedAtAndAlreadyRead(ctx context.Context, issueID int, c sqlConn) (time.Time, bool, error) {
	var updatedAt string
	var read bool
	err := c.QueryRowContext(ctx, `select updatedAt, alreadyRead from issues where id = ?`, issueID).Scan(&updatedAt, &read)
	if err != nil {
		return time.Time{}, false, err
	}
	u, err := parseTime(updatedAt)
	if err != nil {
		return time.Time{}, false, err
	}
	return u, read, nil
}

func findOrCreateQuery(ctx context.Context, query string, tx sqlConn) (int, error) {
	var id int
	err := tx.QueryRowContext(ctx, `select id from queries where query = ?`, query).Scan(&id)
	if err == sql.ErrNoRows {
		res, err := tx.ExecContext(ctx, `insert into queries (query) VALUES (?)`, query)
		if err != nil {
			return 0, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, err
		}
		return (int)(id), nil
	} else if err != nil {
		return 0, err
	}

	return id, nil
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

func OldestIssueTime(ctx context.Context, queryID int) (time.Time, error) {
	return findEdgeIssueTime(ctx, queryID, "asc")
}

func NewestIssueTime(ctx context.Context, queryID int) (time.Time, error) {
	return findEdgeIssueTime(ctx, queryID, "desc")
}

func findEdgeIssueTime(ctx context.Context, queryID int, ascDesc string) (time.Time, error) {
	var t string
	err := Conn.QueryRowContext(ctx, fmt.Sprintf(`
		select
			i.updatedAt
		from
			issues as i,
			channel_issues as ci
		where
			i.id = ci.issueID AND
			ci.queryID = ?
		order by i.updatedAt %s
		limit 1
		;
`, ascDesc), queryID).Scan(&t)
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

type AccountForGitHubAPI struct {
	accessToken string
	id          int
}

func SelectUndeterminedPullRequest(ctx context.Context, accountID int) (id int, owner string, repo string, number int, err error) {
	t := fmtTime(time.Now().Add(-3 * 24 * time.Hour))
	err = Conn.QueryRowContext(ctx, `
		select i.id, i.repoOwner, i.repoName, i.number
		from
			issues as i,
			channel_issues as ci,
			channels as c
		where
			i.id = ci.issueID AND
			ci.channelID = c.id AND
			c.accountID = ? AND
			i.isPullRequest = 1 AND
			i.merged is null AND
			i.closedAt is not null AND
			i.updatedAt > ?
	`, accountID, t).Scan(&id, &owner, &repo, &number)
	return
}

func DetermineMerged(ctx context.Context, issueID int, merged bool) error {
	_, err := Conn.ExecContext(ctx, `
		update issues
		set merged = ?
		where id = ?
	`, merged, issueID)
	return err
}

func CreateAccount(ctx context.Context, p *AccountCreateParam) error {
	_, err := Conn.ExecContext(ctx, `
		insert into accounts(displayName, urlBase, apiUrlBase, accessToken)
		VALUES { ?, ?, ?, ? }
	`, p.DisplayName, p.UrlBase, p.ApiUrlBase, p.AccessToken)
	return err
}

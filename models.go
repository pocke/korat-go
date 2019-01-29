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
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
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

func SelectChannelsUnreadCount(ctx context.Context) ([]*UnreadCount, error) {
	res := make([]*UnreadCount, 0)

	rows, err := gormConn.Raw(`
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
	`).Rows()
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

	var channelIDs []int
	err := gormConn.Table("channel_issues").
		Where("issueID IN (?)", issueIDsStr).
		Pluck("distinct(channelID)", &channelIDs).Error
	if err != nil {
		return nil, errors.WithStack(err)
	}

	res := make([]*UnreadCount, 0)
	channelMap := make(map[int]*UnreadCount, 0)

	for _, cid := range channelIDs {
		c := &UnreadCount{Count: 0, ChannelID: cid}
		res = append(res, c)
		channelMap[c.ChannelID] = c
	}

	subq := gormConn.
		Table("channel_issues").
		Joins("JOIN issues on issues.id = channel_issues.issueID").
		Select("distinct channelID, issueID").
		Where("channelID IN (?)", channelIDs).
		Where("alreadyRead = 0").QueryExpr()
	rows, err := gormConn.Raw(`
		select channelID, count(issueID)
		from (?)
		group by channelID
		`, subq).Rows()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer rows.Close()

	for rows.Next() {
		var channelID int
		var cnt int
		err := rows.Scan(&channelID, &cnt)
		if err != nil {
			return nil, errors.WithStack(err)
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

	User      *UserOld
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

	rows, err := gormConn.Raw(fmt.Sprintf(`
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
	`, additionalConds), q.channelID, q.perPage, (q.page-1)*q.perPage).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		u := &UserOld{}
		i := &IssueOld{
			Labels:    []*LabelOld{},
			Assignees: []*UserOld{},
			User:      u,
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

	rows, err := gormConn.Raw(fmt.Sprintf(`
		select
			l.id, l.name, l.color, l.'default', li.issueID
		from
			labels as l,
			assigned_labels_to_issue as li
		where
			l.id = li.labelID AND
			li.issueID IN (%s)
		;
	`, strings.Join(issueIDs, ", "))).Rows()
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

	rows, err := gormConn.Raw(fmt.Sprintf(`
		select
			u.id, u.login, u.avatarURL, ui.issueID
		from
			github_users as u,
			assigned_users_to_issue as ui
		where
			u.id = ui.userID AND
			ui.issueID IN (%s)
		;
	`, strings.Join(issueIDs, ", "))).Rows()
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

func repoInfoFromIssue(i github.Issue) (string, string) {
	url := i.GetURL()
	m := RepoFromIssueUrlRe.FindStringSubmatch(url)
	return m[1], m[2]
}

func ImportIssues(ctx context.Context, issues []github.Issue, channelID int, query string) error {
	return txGorm(func(tx *gorm.DB) error {
		q := Query{Query: query}
		err := tx.FirstOrCreate(&q, q).Error
		if err != nil {
			return errors.WithStack(err)
		}

		for _, i := range issues {
			repoOwner, repoName := repoInfoFromIssue(i)

			user := i.GetUser()
			userID := user.GetID()
			err := tx.Exec(`
				replace into github_users
				(id, login, avatarURL)
				values (?, ?, ?)
			`, userID, user.GetLogin(), user.GetAvatarURL()).Error
			if err != nil {
				return errors.WithStack(err)
			}

			id := i.GetID()
			issueTmp := Issue{ID: (int)(id)}
			res := tx.First(&issueTmp)
			exist := !res.RecordNotFound()
			var prevUpdatedAt time.Time
			var prevAlreadyRead bool
			if !exist {
				// do nothing
			} else if res.Error != nil {
				return errors.WithStack(res.Error)
			} else {
				prevAlreadyRead = issueTmp.AlreadyRead
				prevUpdatedAt, err = parseTime(issueTmp.UpdatedAt)
				if err != nil {
					return errors.WithStack(err)
				}
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
					return errors.WithStack(err)
				}
			}

			if exist {
				err = tx.Exec(`
					update issues
					set number = ?, title = ?, userID = ?, repoOwner = ?, repoName = ?, state = ?, locked = ?, comments = ?,
					createdAt = ?, updatedAt = ?, closedAt = ?, isPullRequest = ?, body = ?, milestoneID = ?, alreadyRead = ?
					where id = ?
				`, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(),
					createdAt, updatedAt, closedAt, i.IsPullRequest(), i.GetBody(), milestoneID, prevAlreadyRead && prevUpdatedAt.Equal(i.GetUpdatedAt()), id).Error
				if err != nil {
					return errors.WithStack(err)
				}
			} else {
				// If issue is too old, it is marked as read
				alreadyRead := i.GetUpdatedAt().Before(time.Now().Add(-24 * 30 * time.Hour))
				err = tx.Exec(`
					insert into issues
					(id, number, title, userID, repoOwner, repoName, state, locked, comments,
					createdAt, updatedAt, closedAt, isPullRequest, body, alreadyRead, milestoneID)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, id, i.GetNumber(), i.GetTitle(), userID, repoOwner, repoName, i.GetState(), i.GetLocked(), i.GetComments(),
					createdAt, updatedAt, closedAt, i.IsPullRequest(), i.GetBody(), alreadyRead, milestoneID).Error
				if err != nil {
					return errors.WithStack(err)
				}
			}

			if err := importLabels(ctx, i, tx); err != nil {
				return errors.WithStack(err)
			}
			if err := importAssignees(ctx, i, tx); err != nil {
				return errors.WithStack(err)
			}

			err = tx.Exec(`
				replace into channel_issues
				(issueID, channelID, queryID)
				values (?, ?, ?)
			`, id, channelID, q.ID).Error
			if err != nil {
				return errors.WithStack(err)
			}

		}
		return nil
	})
}

func issueUpdatedAtAndAlreadyRead(ctx context.Context, issueID int, c *gorm.DB) (time.Time, bool, error) {
	var updatedAt string
	var read bool
	err := c.Raw(`select updatedAt, alreadyRead from issues where id = ?`, issueID).Row().Scan(&updatedAt, &read)
	if err != nil {
		return time.Time{}, false, err
	}
	u, err := parseTime(updatedAt)
	if err != nil {
		return time.Time{}, false, err
	}
	return u, read, nil
}

func importLabels(ctx context.Context, issue github.Issue, tx *gorm.DB) error {
	issueID := issue.GetID()
	err := tx.Exec(`
			delete from assigned_labels_to_issue
			where issueID = ?
		`, issueID).Error
	if err != nil {
		return err
	}

	for _, label := range issue.Labels {
		labelID := label.GetID()

		err := tx.Exec(`
				replace into labels
				(id, name, color, 'default')
				VALUES (?, ?, ?, ?)
			`, labelID, label.GetName(), label.GetColor(), label.GetDefault()).Error
		if err != nil {
			return err
		}

		err = tx.Exec(`
				insert into assigned_labels_to_issue
				(issueID, labelID)
				VALUES (?, ?)
			`, issueID, labelID).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func insertMilestone(ctx context.Context, milestone *github.Milestone, tx *gorm.DB) error {
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

	err := tx.Exec(`
			replace into milestones
			(id, number, title, description, state, createdAt, updatedAt, closedAt)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, mID, milestone.GetNumber(), milestone.GetTitle(), milestone.GetDescription(), milestone.GetState(), createdAt, updatedAt, closedAt).Error
	if err != nil {
		return err
	}

	return nil
}

func importAssignees(ctx context.Context, issue github.Issue, tx *gorm.DB) error {
	issueID := issue.GetID()
	err := tx.Exec(`
			delete from assigned_users_to_issue
			where issueID = ?
		`, issueID).Error
	if err != nil {
		return err
	}

	for _, user := range issue.Assignees {
		userID := user.GetID()

		err := tx.Exec(`
				replace into github_users
				(id, login, avatarURL)
				VALUES (?, ?, ?)
			`, userID, user.GetLogin(), user.GetAvatarURL()).Error
		if err != nil {
			return err
		}

		err = tx.Exec(`
				insert into assigned_users_to_issue
				(issueID, userID)
				VALUES (?, ?)
			`, issueID, userID).Error
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

func UpdateIssueAlreadyRead(ctx context.Context, issueID int, alreadyRead bool) error {
	return gormConn.Exec(`
		update issues
		set alreadyRead = ?
		where id = ?
	`, alreadyRead, issueID).Error
}

type AccountForGitHubAPI struct {
	accessToken string
	id          int
}

func SelectUndeterminedPullRequest(accountID int) *gorm.DB {
	t := fmtTime(time.Now().Add(-3 * 24 * time.Hour))
	return gormConn.
		Joins("JOIN channel_issues as ci ON ci.issueID = issues.id").
		Joins("JOIN channels as c ON c.id = ci.channelID").
		Where("c.accountID = ?", accountID).
		Where("issues.isPullRequest = 1 AND issues.merged is null AND issues.closedAt is not null AND issues.updatedAt > ?", t).
		Limit(1)
}

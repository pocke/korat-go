package main

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	homedir "github.com/mitchellh/go-homedir"
)

type Issue struct {
	ID            int `gorm:"primary_key"`
	Number        int
	Title         string
	RepoOwner     string `gorm:"column:repoOwner"`
	RepoName      string `gorm:column:repoName`
	State         string
	Locked        bool
	Comments      int
	CreatedAt     string         `gorm:"column:createdAt"`
	UpdatedAt     string         `gorm:"column:updatedAt"`
	ClosedAt      NullStringJSON `gorm:"column:closedAt"`
	IsPullRequest bool           `gorm:"column:isPullRequest"`
	Body          string
	AlreadyRead   bool `gorm:"column:alreadyRead"`
	Merged        NullBoolJSON

	User      *User
	Labels    []*Label
	Assignees []*User
}

type Label struct {
	ID      int
	Name    string
	Color   string
	Default bool
}

type User struct {
	ID        int
	Login     string
	AvatarURL string
}

type Account struct {
	ID          int
	DisplayName string `gorm:"column:displayName"`
	UrlBase     string `gorm:"column:urlBase"`
	ApiUrlBase  string `gorm:"column:apiUrlBase"`
	AccessToken string `gorm:"column:accessToken"`

	Channels []Channel
}

type Channel struct {
	ID          int
	DisplayName string `gorm:"column:displayName"`
	System      sql.NullString
	QueriesRaw  string `gorm:"column:queries"`
	AccountID   int    `gorm:"column:accountID"`

	Account Account
}

func (c Channel) Queries() ([]string, error) {
	res := make([]string, 0)
	err := json.Unmarshal([]byte(c.QueriesRaw), &res)
	return res, err
}

func EdgeIssueTime(queryID int, order string) (time.Time, error) {
	i := &Issue{}
	err := gormConn.Joins("JOIN channel_issues as ci").
		Where("ci.queryID = ?", queryID).
		Order("issues.updatedAt " + order).Limit(1).First(i).Error
	if err != nil {
		return time.Time{}, err
	}

	return parseTime(i.UpdatedAt)
}

func init() {
	fname, err := homedir.Expand("~/.cache/korat/development.sqlite3")
	if err != nil {
		panic(err)
	}
	db, err := gorm.Open("sqlite3", fname)
	if err != nil {
		panic(err)
	}

	// Will not set CreatedAt and UpdatedAt on .Create() call
	db.Callback().Create().Remove("gorm:update_time_stamp")
	// Will not update UpdatedAt on .Save() call
	db.Callback().Update().Remove("gorm:update_time_stamp")
	db.LogMode(true)

	gormConn = db
}

var gormConn *gorm.DB

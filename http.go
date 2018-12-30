package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
)

func StartHTTPServer(port int) {
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"file:///", "http://localhost:32034"},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept},
	}))

	e.GET("/accounts", accountsIndex)
	e.GET("/channels/:channelID/issues", issuesIndex)
	e.PATCH("/issues/:issueID/markAsRead", issuesMarkAsRead)
	e.PATCH("/issues/:issueID/markAsUnread", issuesMarkAsUnread)
	e.Logger.Fatal(e.Start(fmt.Sprintf(":%d", port)))
}

type ResponseAccount struct {
	ID          int
	DisplayName string
	UrlBase     string
	ApiUrlBase  string
	Channels    []ResponseChannel
}

type ResponseChannel struct {
	ID          int
	DisplayName string
	System      sql.NullString
	Queries     []string
}

func accountsIndex(c echo.Context) error {
	chs, err := SelectChannels(c.Request().Context())
	if err != nil {
		return err
	}

	addedAccountIDs := []int{}
	res := make([]*ResponseAccount, 0, 1)
	for _, ch := range chs {
		rch := ResponseChannel{
			ID:          ch.id,
			DisplayName: ch.displayName,
			System:      ch.system,
			Queries:     ch.queries,
		}

		if idx := idxIntSlice(addedAccountIDs, ch.account.id); idx != -1 {
			res[idx].Channels = append(res[idx].Channels, rch)
		} else {
			a := ch.account
			ra := &ResponseAccount{
				ID:          a.id,
				DisplayName: a.displayName,
				UrlBase:     a.urlBase,
				ApiUrlBase:  a.apiUrlBase,
				Channels:    []ResponseChannel{rch},
			}
			addedAccountIDs = append(addedAccountIDs, ch.account.id)
			res = append(res, ra)
		}
	}
	return c.JSON(http.StatusOK, res)
}

func issuesIndex(c echo.Context) error {
	channelID, err := strconv.Atoi(c.Param("channelID"))
	if err != nil {
		return err
	}
	// TODO
	page := 1
	perPage := 100
	issues, err := SelectIssues(c.Request().Context(), channelID, page, perPage)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, issues)

	return nil
}
func issuesMarkAsRead(c echo.Context) error {
	issueID, err := strconv.Atoi(c.Param("issueID"))
	if err != nil {
		return err
	}

	err = UpdateIssueAlreadyRead(c.Request().Context(), issueID, true)
	if err != nil {
		return err
	}

	return c.NoContent(http.StatusOK)
}

func issuesMarkAsUnread(c echo.Context) error {
	issueID, err := strconv.Atoi(c.Param("issueID"))
	if err != nil {
		return err
	}

	err = UpdateIssueAlreadyRead(c.Request().Context(), issueID, false)
	if err != nil {
		return err
	}

	return c.NoContent(http.StatusOK)
}

func idxIntSlice(arr []int, obj int) int {
	for idx, v := range arr {
		if v == obj {
			return idx
		}
	}
	return -1
}

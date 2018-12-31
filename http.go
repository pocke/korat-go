package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"github.com/pkg/errors"
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

	e.GET("/ws", wsHandler)
	e.Logger.Fatal(e.Start(fmt.Sprintf(":%d", port)))
}

type ResponseAccount struct {
	ID          int
	DisplayName string
	UrlBase     string
	ApiUrlBase  string
	Channels    []*ResponseChannel
}

type ResponseChannel struct {
	ID          int
	DisplayName string
	System      sql.NullString
	Queries     []string
}

func accountsIndex(c echo.Context) error {
	accounts, err := SelectAccountForAPI(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, accounts)
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

var upgrader = websocket.Upgrader{}

func wsHandler(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	go func() {
		for {
			if _, _, err := ws.NextReader(); err != nil {
				ws.Close()
				break
			}
		}
	}()

	for {
		// TODO: write
	}

	return errors.New("Unreachable")
}

func idxIntSlice(arr []int, obj int) int {
	for idx, v := range arr {
		if v == obj {
			return idx
		}
	}
	return -1
}

package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
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
	return handleAlreadyRead(c, true)
}

func issuesMarkAsUnread(c echo.Context) error {
	return handleAlreadyRead(c, false)
}

func handleAlreadyRead(c echo.Context, read bool) error {
	issueID, err := strconv.Atoi(c.Param("issueID"))
	if err != nil {
		return err
	}

	err = UpdateIssueAlreadyRead(c.Request().Context(), issueID, read)
	if err != nil {
		return err
	}

	cnts, err := UnreadCountForIssue(c.Request().Context(), issueID)
	if err != nil {
		return err
	}
	for _, cnt := range cnts {
		unreadCountNotifier.Notify(cnt)
	}

	return c.NoContent(http.StatusOK)
}

var upgrader = websocket.Upgrader{
	CheckOrigin: checkOriginWS,
}

func checkOriginWS(r *http.Request) bool {
	return true
	origin := r.Header["Origin"]
	if len(origin) == 0 {
		return true
	}
	u, err := url.Parse(origin[0])
	if err != nil {
		return false
	}
	return u.Host == "localhost:32034"
}

type WsMessage struct {
	Type    string
	Payload interface{}
}

const WsTypeUnreadCount = "UnreadCount"

func wsHandler(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	closeCh := make(chan struct{})

	go func() {
		for {
			if _, _, err := ws.NextReader(); err != nil {
				ws.Close()
				close(closeCh)
				break
			}
		}
	}()

	ch := unreadCountNotifier.Subscribe()
	defer unreadCountNotifier.Unsubscribe(ch)

	initCnts, err := SelectChannelsUnreadCount(c.Request().Context())
	if err != nil {
		return err
	}
	for _, initCnt := range initCnts {
		if err := ws.WriteJSON(WsMessage{Type: WsTypeUnreadCount, Payload: initCnt}); err != nil {
			return err
		}
	}

	for {
		select {
		case count := <-ch:
			err := ws.WriteJSON(WsMessage{Type: WsTypeUnreadCount, Payload: count})
			if err != nil {
				return err
			}
		case <-closeCh:
			return nil
		}
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

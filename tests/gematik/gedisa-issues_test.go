package tests

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/matrix-org/complement"
	"github.com/matrix-org/complement/b"
	"github.com/matrix-org/gomatrixserverlib/spec"

	"github.com/matrix-org/complement/client"
	"github.com/matrix-org/complement/helpers"
	"github.com/matrix-org/complement/match"
	"github.com/matrix-org/complement/must"
	"github.com/tidwall/gjson"
)

func TestGedisaIssueMissingMessages(t *testing.T) {
	deployment := complement.Deploy(t, 2)
	defer deployment.Destroy(t)

	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{LocalpartSuffix: "alice"})
	charlie := deployment.Register(t, "hs2", helpers.RegistrationOpts{LocalpartSuffix: "charlie"})

	roomID := alice.MustCreateRoom(t, map[string]interface{}{
		"preset": "private_chat",
	})
	alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(alice.UserID, roomID))

	// We will call this messageA
	messageID_A := alice.SendEventSynced(t, roomID, b.Event{
		Type: "m.room.message",
		Content: map[string]interface{}{
			"msgtype": "m.text",
			"body":    "Message Before Invite",
		},
	})

	// The planned procedure:
	//   Create the room with alice, and send messageA into it
	//      (Because the messageA is a previous event of the join, but not state in and of itself, it will not be pulled automatically)
	//	 Invite charlie to the room
	//	 Start joining charlie to the room
	//   Send a messageB just after the join has started but not finished, so it will not be referenced in the prev_events of the join
	//   Begin the sync cycle to watch for the messages(should see the join and then the message some time later)
	//
	// Use the fact that goroutines seem to occur in a LIFO manner

	// grab that first token to start the process
	var char_since_token string
	_, char_since_token = charlie.MustSync(t, client.SyncReq{TimeoutMillis: "25000"})

	fmt.Println("ALICE SENDING CHARLIES INVITES*************")
	alice.MustInviteRoom(t, roomID, charlie.UserID)
	fmt.Println("*************ALICE DONE INVITING CHARLIE")

	fmt.Println("CHARLIE WATCHING FOR INVITE*************")
	char_since_token = charlie.MustSyncUntil(t, client.SyncReq{Since: char_since_token, TimeoutMillis: "25000"}, client.SyncInvitedTo(charlie.UserID, roomID))
	fmt.Println("*************CHARLIE JUST SAW HE WAS INVITED")

	// we need the messageB event ID to find out if it shows up in the /messages response
	messageB_ID_chan := make(chan string)

	go func() {
		fmt.Println("CHARLIE STARTING JOIN*************")
		charlie.MustJoinRoom(t, roomID, []spec.ServerName{deployment.GetFullyQualifiedHomeserverName(t, "hs1")})
		fmt.Println("*************CHARLIE JUST JOINED THE ROOM")
	}()

	go func(event_id_chan chan string) {
		// We will call this messageB
		time.Sleep(90 * time.Millisecond)

		fmt.Println("ALICE SENDING MESSAGE B*************")
		// by not using the safe version of send event, it allows for not waiting for processing to finish
		// event_id_chan <- alice.SendEventSynced(t, roomID, b.Event{
		event_id_chan <- alice.Unsafe_SendEventUnsynced(t, roomID, b.Event{
			Type: "m.room.message",
			Content: map[string]interface{}{
				"msgtype": "m.text",
				"body":    "Message During Join",
			},
		})
		fmt.Println("*************ALICE DONE SENDING MESSAGE B")

	}(messageB_ID_chan)

	fmt.Println("CHARLIE WATCHING FOR JOIN*************")
	_ = charlie.MustSyncUntil(t, client.SyncReq{Since: char_since_token, TimeoutMillis: "25000"}, client.SyncJoinedTo(charlie.UserID, roomID))
	fmt.Println("*************CHARLIE JUST SAW HE WAS JOINED")
	// messageID_A = <-messageB_ID_chan
	// fmt.Println("CHARLIE WATCHING FOR MESSAGE*************")
	// _ = charlie.MustSyncUntil(t, client.SyncReq{Since: char_since_token, TimeoutMillis: "25000"}, client.SyncTimelineHasEventID(roomID, messageID_A))
	// fmt.Println("*************CHARLIE JUST SAW MESSAGE")

	// This is just the standard backwards /messages after join completes request
	// queryParams := url.Values{}
	// queryParams.Set("dir", "b")
	// queryParams.Set("limit", "100")
	// queryParams.Set("raw", gjson.True.String())
	// fmt.Println("CHARLIE MAKING MESSAGES*************")
	// charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
	// fmt.Println("*************CHARLIE RECEIVED MESSAGES RESPONSE")

	// body := client.ParseJSON(t, res)
	// result := gjson.ParseBytes(body)
	// endToken := result.Get("end").Str
	// queryParams.Set("from", endToken)
	// fmt.Println("CHARLIE MAKING SECOND MESSAGES*************")

	// charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
	// fmt.Println("*************CHARLIE RECEIVED SECOND MESSAGES RESPONSE")

	// Now watch for the message event from earlier to show up
	// fmt.Println("CHARLIE WATCHING FOR MESSAGE*************")
	// _ = charlie.MustSyncUntil(t, client.SyncReq{Since: char_since_token, TimeoutMillis: "29000"}, client.SyncTimelineHasEventID(roomID, messageID))
	// fmt.Println("*************CHARLIE JUST SAW MESSAGE")

	// All of the above will complete before we move on
	// sync_response := <-char_sync_response
	// beforeToken := sync_response.Get("rooms").Get("join").Get(roomID).Get("timeline").Get("prev_batch").Str
	// t.Logf("beforeToken: " + beforeToken)

	// We need to simulate this more reliably, so we will employ a 'sleep' to help it along.
	time.Sleep(1000 * time.Millisecond)

	queryParams := url.Values{}
	queryParams.Set("dir", "b")
	queryParams.Set("limit", "100")
	queryParams.Set("raw", gjson.True.String())
	fmt.Println("CHARLIE MAKING STANDARD MESSAGES REQUEST*************")
	res := charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
	fmt.Println("*************CHARLIE RECEIVED STANDARD MESSAGES RESPONSE")
	must.MatchResponse(t, res, match.HTTPResponse{
		StatusCode: http.StatusOK,
		JSON: []match.JSON{
			match.JSONCheckOff(
				// look in this array
				"chunk",
				// for these items
				[]interface{}{messageID_A, <-messageB_ID_chan},
				// and map them first into this format
				match.CheckOffMapper(func(r gjson.Result) interface{} {
					// fmt.Print(r)
					return r.Get("event_id").Str
				}), match.CheckOffAllowUnwanted(),
			),
		},
	})
}

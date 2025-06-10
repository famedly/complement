package tests

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/matrix-org/complement"
	"github.com/matrix-org/complement/b"
	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"

	"github.com/matrix-org/complement/client"
	"github.com/matrix-org/complement/federation"
	"github.com/matrix-org/complement/helpers"
	"github.com/matrix-org/complement/match"
	"github.com/matrix-org/complement/must"
	"github.com/tidwall/gjson"
)

// This test ensures that invite rejections are correctly sent out over federation.
//
// We start with two users in a room - alice@hs1, and 'delia' on the Complement test server.
// alice sends an invite to charlie@hs2, which he rejects.
// We check that delia sees the rejection.
func TestFederationRejectInvite(t *testing.T) {
	deployment := complement.Deploy(t, 2)
	defer deployment.Destroy(t)
	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{})
	charlie := deployment.Register(t, "hs2", helpers.RegistrationOpts{})

	// we'll awaken this Waiter when we receive a membership event for Charlie
	var waiter *helpers.Waiter

	srv := federation.NewServer(t, deployment,
		federation.HandleKeyRequests(),
		federation.HandleTransactionRequests(func(ev gomatrixserverlib.PDU) {
			sk := "<nil>"
			if ev.StateKey() != nil {
				sk = *ev.StateKey()
			}
			t.Logf("Received PDU %s/%s", ev.Type(), sk)
			if waiter != nil && ev.Type() == "m.room.member" && ev.StateKeyEquals(charlie.UserID) {
				waiter.Finish()
			}
		}, nil),
	)
	srv.UnexpectedRequestsAreErrors = false
	cancel := srv.Listen()
	defer cancel()
	delia := srv.UserID("delia")

	// Alice creates the room, and delia joins
	roomID := alice.MustCreateRoom(t, map[string]interface{}{"preset": "public_chat"})
	room := srv.MustJoinRoom(t, deployment, deployment.GetFullyQualifiedHomeserverName(t, "hs1"), roomID, delia)

	// Alice invites Charlie; Delia should see the invite
	waiter = helpers.NewWaiter()
	alice.MustInviteRoom(t, roomID, charlie.UserID)
	waiter.Wait(t, 5*time.Second)
	room.MustHaveMembershipForUser(t, charlie.UserID, "invite")

	// Charlie rejects the invite; Delia should see the rejection.
	waiter = helpers.NewWaiter()
	charlie.MustLeaveRoom(t, roomID)
	waiter.Wait(t, 5*time.Second)
	room.MustHaveMembershipForUser(t, charlie.UserID, "leave")
}

func TestFederationRoomsInviteMessages(t *testing.T) {
	deployment := complement.Deploy(t, 2)
	defer deployment.Destroy(t)

	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{LocalpartSuffix: "alice"})
	charlie := deployment.Register(t, "hs2", helpers.RegistrationOpts{LocalpartSuffix: "charlie"})

	roomID := alice.MustCreateRoom(t, map[string]interface{}{
		"preset": "private_chat",
	})
	alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(alice.UserID, roomID))

	eventID := alice.SendEventSynced(t, roomID, b.Event{
		Type: "m.room.message",
		Content: map[string]interface{}{
			"msgtype": "m.text",
			"body":    "Message Before Invite",
		},
	})

	// The planned procedure:
	//   We have to join charlie to the room
	// 	 As soon as the join is complete, need to fire off asynchronously:
	//	   The special /messages request, and
	//	   The regular /messages request
	//	 Take care that the special one tries to go first, as it is expected to trigger backfill, and
	//		is blocked by the partial stating of the room
	//
	// Use the fact that goroutines seem to occur in a LIFO manner, and gate some responses on charlie's token(for sync)

	// Use this to keep track of the sync token during incremental syncs, like a real client would
	char_since_token := make(chan string, 2)
	// We will need the whole sync response to parse out the token
	char_sync_response := make(chan gjson.Result)

	// grab that first token to start the process
	_, _char_since_token := charlie.MustSync(t, client.SyncReq{TimeoutMillis: "25000"})
	char_since_token <- _char_since_token

	fmt.Println("ALICE SENDING CHARLIES INVITES*************")
	alice.MustInviteRoom(t, roomID, charlie.UserID)
	fmt.Println("*************ALICE DONE INVITING CHARLIE")

	go func(char_token chan string, char_response chan gjson.Result) {
		fmt.Println("CHARLIE WATCHING FOR JOIN*************")
		// This function is an exact replica of MustSyncUntil() with the difference of retrieving the response as well as the token
		response, next_batch := charlie.MustSyncUntilAndReturnResponse(t, client.SyncReq{Since: <-char_token, TimeoutMillis: "25000"}, client.SyncJoinedTo(charlie.UserID, roomID))
		fmt.Println("*************CHARLIE JUST SAW HE WAS JOINED")

		char_response <- response
		char_token <- next_batch
	}(char_since_token, char_sync_response)

	go func(char_token chan string) {
		char_token <- charlie.MustSyncUntil(t, client.SyncReq{Since: <-char_token, TimeoutMillis: "25000"}, client.SyncInvitedTo(charlie.UserID, roomID))

		fmt.Println("CHARLIE STARTING JOIN*************")
		charlie.MustJoinRoom(t, roomID, []spec.ServerName{deployment.GetFullyQualifiedHomeserverName(t, "hs1")})
		fmt.Println("*************CHARLIE JUST JOINED THE ROOM")

		go func() {
			// This is just the standard backwards /messages after join completes request
			queryParams := url.Values{}
			queryParams.Set("dir", "b")
			queryParams.Set("limit", "100")
			// queryParams.Set("raw", gjson.True.String())
			fmt.Println("CHARLIE MAKING MESSAGES*************")
			charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
			fmt.Println("*************CHARLIE RECEIVED MESSAGES RESPONSE")

			// body := client.ParseJSON(t, res)
			// result := gjson.ParseBytes(body)
			// endToken := result.Get("end").Str
			// queryParams.Set("from", endToken)
			// fmt.Println("CHARLIE MAKING SECOND MESSAGES*************")

			// charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
			// fmt.Println("*************CHARLIE RECEIVED SECOND MESSAGES RESPONSE")

		}()
		go func() {
			// {"room":{"state":{"types":["m.room.member"]},"timeline":{"types":["m.room.member"]}}}
			// `{"state":{"types":["m.room.member"]},"timeline":{"types":["m.room.member"]}}`
			// {\"room\":{\"state\":{\"types\":[\"m.room.member\"]},\"timeline\":{\"types\":[\"m.room.member\"]}}}
			// old version: %7B%22room%22%3A%7B%22state%22%3A%7B%22types%22%3A%5B%22m.room.member%22%5D%7D%2C%22timeline%22%3A%7B%22types%22%3A%5B%22m.room.member%22%5D%7D%7D%7D
			// new version: %7B%22state%22%3A%7B%22types%22%3A%5B%22m.room.member%22%5D%7D%2C%22timeline%22%3A%7B%22types%22%3A%5B%22m.room.member%22%5D%7D%7D
			// here is the gimpy /messages request that sometimes borks
			queryParams := url.Values{}
			queryParams.Set("dir", "b")
			queryParams.Set("filter", `{"types":["m.room.member"]}`)
			// queryParams.Set("raw", gjson.True.String())
			fmt.Println("CHARLIE MAKING SPECIAL MESSAGES REQUEST*************")
			charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
			fmt.Println("*************CHARLIE RECEIVED RESPONSE TO SPECIAL MESSAGES REQUEST")
			// body := client.ParseJSON(t, res)
			// result := gjson.ParseBytes(body)
			// endToken := result.Get("end").Str
			// fmt.Println("CHARLIE MAKING SECOND SPECIAL MESSAGES REQUEST*************")
			// queryParams.Set("from", endToken)
			// charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
			// fmt.Println("*************CHARLIE RECEIVED RESPONSE TO SECOND SPECIAL MESSAGES REQUEST")
		}()
	}(char_since_token)

	// All of the above will complete before we move on
	sync_response := <-char_sync_response
	beforeToken := sync_response.Get("rooms").Get("join").Get(roomID).Get("timeline").Get("prev_batch").Str
	t.Logf("beforeToken: " + beforeToken)

	queryParams := url.Values{}
	queryParams.Set("dir", "b")
	queryParams.Set("from", beforeToken)
	// queryParams.Set("raw", gjson.True.String())
	res := charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
	must.MatchResponse(t, res, match.HTTPResponse{
		StatusCode: http.StatusOK,
		JSON: []match.JSON{
			match.JSONCheckOff(
				// look in this array
				"chunk",
				// for these items
				[]interface{}{eventID},
				// and map them first into this format
				match.CheckOffMapper(func(r gjson.Result) interface{} {
					// fmt.Print(r)
					return r.Get("event_id").Str
				}), match.CheckOffAllowUnwanted(),
			),
		},
	})
}

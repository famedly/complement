package tests

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/matrix-org/complement"
	"github.com/matrix-org/complement/b"
	"github.com/matrix-org/gomatrixserverlib/spec"

	"github.com/matrix-org/complement/client"
	"github.com/matrix-org/complement/helpers"
	"github.com/matrix-org/complement/match"
	"github.com/matrix-org/complement/must"
	"github.com/tidwall/gjson"
)

func TestFederationRoomsMessagesAfterJoin(t *testing.T) {
	// The planned procedure:
	//	Alice creates a room
	//  Charlie has his gematik permissions set to 'allow all' but restricting Alice
	//  Alice invites Bob, which should succeed
	//  Alice invites Charlie, which should not succeed
	//  Bob invites Charlie, which should succeed
	//  Charlie sees the invite come down the /sync response(grab the token here for future incremental syncs)
	//  Charlie begins joining the room
	//  Charlie finishes joining the room(the /join response returns) and issues a /messages request

	//  * Using the /join response as the /messages trigger seems to trigger /backfill before the room is done being un-partial-stated
	// 		so will be paused until that is finished(due to getting a list of hosts in the room to backfill from). In practice, this
	// 		does not seem to cause the issue directly.
	//
	// Use the fact that goroutines seem to occur in a LIFO manner, and gate some requests on various responses

	deployment := complement.Deploy(t, 2)
	defer deployment.Destroy(t)

	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{LocalpartSuffix: "alice"})
	bob := deployment.Register(t, "hs1", helpers.RegistrationOpts{LocalpartSuffix: "bob"})
	charlie := deployment.Register(t, "hs2", helpers.RegistrationOpts{LocalpartSuffix: "charlie"})
	// Logging in is handled by the /register above. This /register action does not call the login spam checker callback, so the default
	// permissions are not populated(but would be on the first invite check). Since this permission structure needs to exist before the
	// first invite, we will just inject it directly.

	acct_data := make(map[string]interface{})
	acct_data["defaultSetting"] = "allow all"
	acct_data["userExceptions"] = map[string]interface{}{alice.UserID: map[string]interface{}{}}

	charlie.MustSetGlobalAccountData(t, "de.gematik.tim.account.permissionconfig.epa.v1", acct_data)

	roomID := alice.MustCreateRoom(t, map[string]interface{}{
		"preset": "private_chat",
		"name":   "TKTest_room_name",
		"creation_content": map[string]interface{}{
			"type": "de.gematik.tim.roomtype.default.v1",
		},
		"initial_state": []interface{}{
			// {"type":"m.room.history_visibility","content":{"history_visibility":"invited"},"state_key":""}
			map[string]interface{}{
				"type":      "m.room.history_visibility",
				"state_key": "",
				"content": map[string]interface{}{
					"history_visibility": "invited",
				},
			},
			map[string]interface{}{
				"type":      "de.gematik.tim.room.name",
				"state_key": "",
				"content": map[string]interface{}{
					"name": "TKTest_room_name_de_gematik",
				},
			},
			map[string]interface{}{
				"type":      "de.gematik.tim.room.default.v1",
				"state_key": "",
				"content":   map[string]interface{}{},
			},
			map[string]interface{}{
				"type":      "de.gematik.tim.room.topic",
				"state_key": "",
				"content": map[string]interface{}{
					"topic": "",
				},
			},
		},
	})
	alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(alice.UserID, roomID))

	eventID := alice.SendEventSynced(t, roomID, b.Event{
		Type: "m.room.message",
		Content: map[string]interface{}{
			"msgtype": "m.text",
			"body":    "Message Before Invites",
		},
	})

	// Use this to keep track of the sync token during incremental syncs, like a real client would
	char_since_token := make(chan string, 2)
	// We will need the whole sync response to parse out the token
	char_sync_response := make(chan gjson.Result)
	// further more, gate the testing finishing on the plain /messages request. This helps insure that the test logs are complete
	// Could probably use a Waiter, but I don't know enough about them to do so quickly
	char_messages_response := make(chan *http.Response)

	// grab that first token to start the process
	_, _char_since_token := charlie.MustSync(t, client.SyncReq{TimeoutMillis: "25000"})
	// poke it into the chan so that goroutine can commence
	char_since_token <- _char_since_token

	// Sorry for all the logging bits, but when we use the goroutines below it helps to show when the request starts versus when it returns
	alice.MustInviteRoom(t, roomID, bob.UserID)
	bob.MustSyncUntil(t, client.SyncReq{TimeoutMillis: "25000"}, client.SyncInvitedTo(bob.UserID, roomID))
	bob.MustJoinRoom(t, roomID, []spec.ServerName{deployment.GetFullyQualifiedHomeserverName(t, "hs1")})

	fmt.Println("ALICE SENDING CHARLIES INVITE, EXPECT FAIL*************")
	alice.Must403FailInviteRoom(t, roomID, charlie.UserID)
	fmt.Println("*************ALICE DONE INVITING CHARLIE")

	fmt.Println("BOB SENDING CHARLIES INVITE*************")
	bob.MustInviteRoom(t, roomID, charlie.UserID)
	fmt.Println("*************BOB DONE INVITING CHARLIE")

	go func(char_token chan string, char_response chan gjson.Result) {
		fmt.Println("CHARLIE WATCHING FOR JOIN*************")
		// This function is an exact replica of MustSyncUntil() with the difference of retrieving the response as well as the token
		response, next_batch := charlie.MustSyncUntilAndReturnResponse(t, client.SyncReq{Since: <-char_token, TimeoutMillis: "25000"}, client.SyncJoinedTo(charlie.UserID, roomID))
		fmt.Println("*************CHARLIE JUST SAW HE WAS JOINED")
		// _ = charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "state"})
		char_response <- response
		char_token <- next_batch
	}(char_since_token, char_sync_response)

	go func(char_token chan string, char_messages_response chan *http.Response) {
		// The joining of the room must be asynchronous to everything else. The join must start but we do not
		// wait for it to finish to run the /sync request in the other goroutine. /messages is checked here after the join says it is done
		// instead of basing on what /sync tells us.
		fmt.Println("CHARLIE WATCHING FOR INVITE*************")
		char_token <- charlie.MustSyncUntil(t, client.SyncReq{Since: <-char_token, TimeoutMillis: "25000"}, client.SyncInvitedTo(charlie.UserID, roomID))
		fmt.Println("*************CHARLIE JUST SAW HE WAS INVITED")

		fmt.Println("CHARLIE STARTING JOIN*************")
		charlie.MustJoinRoom(t, roomID, []spec.ServerName{deployment.GetFullyQualifiedHomeserverName(t, "hs1")})
		fmt.Println("*************CHARLIE JUST JOINED THE ROOM")

		// This is just the standard backwards /messages after join completes request
		queryParams := url.Values{}
		queryParams.Set("dir", "b")
		queryParams.Set("limit", "100")
		// Neat bit of undocumented query arg on this endpoint, 'raw' will return the federation version of the event in the response.
		// queryParams.Set("raw", gjson.True.String())
		fmt.Println("CHARLIE MAKING MESSAGES*************")
		char_messages_response <- charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
		fmt.Println("*************CHARLIE RECEIVED MESSAGES RESPONSE")

	}(char_since_token, char_messages_response)

	// All of the above will complete before we move on
	sync_response := <-char_sync_response
	<-char_messages_response

	// We only bother with this still as it provides a convenient way to pass/fail the test, and it's good to visibly inspect
	// if the two messages requests return the same material
	beforeToken := sync_response.Get("rooms").Get("join").Get(roomID).Get("timeline").Get("prev_batch").Str
	t.Logf("beforeToken: " + beforeToken)

	queryParams := url.Values{}
	queryParams.Set("dir", "b")
	queryParams.Set("limit", "100")
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
	t.Logf("************DONE**************")
}

func TestFederationRoomsMessagesAfterSync(t *testing.T) { // The planned procedure:
	//	Alice creates a room
	//  Charlie has his gematik permissions set to 'allow all' but restricting Alice
	//  Alice invites Bob, which should succeed
	//  Alice invites Charlie, which should not succeed
	//  Bob invites Charlie, which should succeed
	//  Charlie sees the invite come down the /sync response(grab the token here for future incremental syncs)
	//  Charlie begins joining the room(but does not finish, this is inside a goroutine)
	//  Charlie sees the join on the /sync response, and issues a /messages request(this is inside a different goroutine)
	//
	// Use the fact that goroutines seem to occur in a LIFO manner, and gate some requests on various responses

	deployment := complement.Deploy(t, 2)
	defer deployment.Destroy(t)

	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{LocalpartSuffix: "alice"})
	bob := deployment.Register(t, "hs1", helpers.RegistrationOpts{LocalpartSuffix: "bob"})
	charlie := deployment.Register(t, "hs2", helpers.RegistrationOpts{LocalpartSuffix: "charlie"})
	// Logging in is handled by the /register above. This /register action does not call the login spam checker callback, so the default
	// permissions are not populated(but would be on the first invite check). Since this permission structure needs to exist before the
	// first invite, we will just inject it directly.

	acct_data := make(map[string]interface{})
	acct_data["defaultSetting"] = "allow all"
	acct_data["userExceptions"] = map[string]interface{}{alice.UserID: map[string]interface{}{}}

	charlie.MustSetGlobalAccountData(t, "de.gematik.tim.account.permissionconfig.epa.v1", acct_data)

	roomID := alice.MustCreateRoom(t, map[string]interface{}{
		"preset": "private_chat",
		"name":   "TKTest_room_name",
		"creation_content": map[string]interface{}{
			"type": "de.gematik.tim.roomtype.default.v1",
		},
		"initial_state": []interface{}{
			map[string]interface{}{
				"type":      "de.gematik.tim.room.name",
				"state_key": "",
				"content": map[string]interface{}{
					"name": "TKTest_room_name_de_gematik",
				},
			},
			map[string]interface{}{
				"type":      "de.gematik.tim.room.default.v1",
				"state_key": "",
				"content":   map[string]interface{}{},
			},
			map[string]interface{}{
				"type":      "de.gematik.tim.room.topic",
				"state_key": "",
				"content": map[string]interface{}{
					"topic": "",
				},
			},
		},
	})
	alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(alice.UserID, roomID))

	eventID := alice.SendEventSynced(t, roomID, b.Event{
		Type: "m.room.message",
		Content: map[string]interface{}{
			"msgtype": "m.text",
			"body":    "Message Before Invites",
		},
	})

	// Use this to keep track of the sync token during incremental syncs, like a real client would
	char_since_token := make(chan string, 2)
	// We will need the whole sync response to parse out the token
	char_sync_response := make(chan gjson.Result)
	// further more, gate the testing finishing on the plain /messages request. This helps insure that the test logs are complete
	// Could probably use a Waiter, but I don't know enough about them to do so quickly
	char_messages_response := make(chan *http.Response)

	// grab that first token to start the process
	_, _char_since_token := charlie.MustSync(t, client.SyncReq{TimeoutMillis: "25000"})
	// poke it into the chan so that goroutine can commence
	char_since_token <- _char_since_token

	// Sorry for all the logging bits, but when we use the goroutines below it helps to show when the request starts versus when it returns
	alice.MustInviteRoom(t, roomID, bob.UserID)
	bob.MustSyncUntil(t, client.SyncReq{TimeoutMillis: "25000"}, client.SyncInvitedTo(bob.UserID, roomID))
	bob.MustJoinRoom(t, roomID, []spec.ServerName{deployment.GetFullyQualifiedHomeserverName(t, "hs1")})

	fmt.Println("ALICE SENDING CHARLIES INVITE, EXPECT FAIL*************")
	alice.Must403FailInviteRoom(t, roomID, charlie.UserID)
	fmt.Println("*************ALICE DONE INVITING CHARLIE")

	fmt.Println("BOB SENDING CHARLIES INVITE*************")
	bob.MustInviteRoom(t, roomID, charlie.UserID)
	fmt.Println("*************BOB DONE INVITING CHARLIE")

	go func(char_token chan string, char_response chan gjson.Result, char_messages_response chan *http.Response) {
		fmt.Println("CHARLIE WATCHING FOR JOIN*************")
		// This function is an exact replica of MustSyncUntil() with the difference of retrieving the response as well as the token
		response, next_batch := charlie.MustSyncUntilAndReturnResponse(t, client.SyncReq{Since: <-char_token, TimeoutMillis: "25000"}, client.SyncJoinedTo(charlie.UserID, roomID))
		fmt.Println("*************CHARLIE JUST SAW HE WAS JOINED")

		char_response <- response
		char_token <- next_batch

		queryParams := url.Values{}
		queryParams.Set("dir", "b")
		queryParams.Set("limit", "100")
		// Neat bit of undocumented query arg on this endpoint, 'raw' will return the federation version of the event in the response.
		// queryParams.Set("raw", gjson.True.String())
		fmt.Println("CHARLIE MAKING MESSAGES*************")
		char_messages_response <- charlie.Do(t, "GET", []string{"_matrix", "client", "v3", "rooms", roomID, "messages"}, client.WithQueries(queryParams))
		fmt.Println("*************CHARLIE RECEIVED MESSAGES RESPONSE")

	}(char_since_token, char_sync_response, char_messages_response)

	go func(char_token chan string) {
		// The joining of the room must be asynchronous to everything else. The join must start but we do not
		// wait for it to finish. Instead, we watch for the /sync response to say the room is joined in the other goroutine.
		fmt.Println("CHARLIE WATCHING FOR INVITE*************")
		char_token <- charlie.MustSyncUntil(t, client.SyncReq{Since: <-char_token, TimeoutMillis: "25000"}, client.SyncInvitedTo(charlie.UserID, roomID))
		fmt.Println("*************CHARLIE JUST SAW HE WAS INVITED")

		fmt.Println("CHARLIE STARTING JOIN*************")
		charlie.MustJoinRoom(t, roomID, []spec.ServerName{deployment.GetFullyQualifiedHomeserverName(t, "hs1")})
		fmt.Println("*************CHARLIE JUST JOINED THE ROOM")

	}(char_since_token)

	// All of the above will complete before we move on
	sync_response := <-char_sync_response
	<-char_messages_response

	// We only bother with this still as it provides a convenient way to pass/fail the test, and it's good to visibly inspect
	// if the two messages requests return the same material
	beforeToken := sync_response.Get("rooms").Get("join").Get(roomID).Get("timeline").Get("prev_batch").Str
	t.Logf("beforeToken: " + beforeToken)

	queryParams := url.Values{}
	queryParams.Set("dir", "b")
	queryParams.Set("limit", "100")
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
	t.Logf("************DONE**************")

}

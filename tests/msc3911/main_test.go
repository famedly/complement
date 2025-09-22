package tests

import (
	"bytes"
	"testing"

	"github.com/matrix-org/complement"
	"github.com/matrix-org/complement/b"
	"github.com/matrix-org/complement/client"
	"github.com/matrix-org/complement/internal/data"
)

func TestMain(m *testing.M) {
	complement.TestMain(m, "msc3911")
}

// Create a room with a history visibility as specificed
func MustCreateRoomWithHistoryVisibility(t *testing.T, creatingUserClient *client.CSAPI, historyVisiblity string) string {
	// t.Helper()
	roomID := creatingUserClient.MustCreateRoom(t, map[string]interface{}{
		"preset":       "public_chat",
		"name":         "Room",
		"room_version": "11",
		"initial_state": []map[string]interface{}{
			{
				"type":      "m.room.history_visibility",
				"state_key": "",
				"content": map[string]interface{}{
					"history_visibility": historyVisiblity,
				},
			},
		},
	})

	return roomID
}

// testing helper to send a message event of type m.image into a room with attached media. Returns the mxcUri and the
// event ID of the message event
func MustUploadMediaAttachToMessageEventAndSendIntoRoom(t *testing.T, sendingUser *client.CSAPI, roomID string) (string, string) {
	mxcUri := sendingUser.MustUploadContentRestricted(t, data.MatrixPng, "test.png", "img/png")
	picture_message := b.Event{
		Type: "m.room.message",
		Content: map[string]interface{}{
			"msgtype": "m.image",
			"body":    "test.png",
			"url":     mxcUri,
		},
		Sender: sendingUser.UserID,
	}
	// alice sends message with extra query parameter
	event_id := sendingUser.SendEventWithAttachedMediaSynced(t, roomID, picture_message, mxcUri)
	return mxcUri, event_id
}

// testing helper to send a state membership event with attached media into a room. Returns the mxcUri and the event ID
// of the membership event
func MustUploadMediaAttachToMembershipEventAndSendIntoRoom(t *testing.T, sendingUser *client.CSAPI, roomID string) (string, string) {
	mxcUri := sendingUser.MustUploadContentRestricted(t, data.MatrixPng, "test.png", "img/png")
	picture_message := b.Event{
		Type: "m.room.member",
		Content: map[string]interface{}{
			"membership": "join",
			"avatar_url": mxcUri,
		},
		Sender:   sendingUser.UserID,
		StateKey: &sendingUser.UserID,
	}
	// alice sends message with extra query parameter
	event_id := sendingUser.SendEventWithAttachedMediaSynced(t, roomID, picture_message, mxcUri)
	return mxcUri, event_id
}

// Have userLooking retrieve the membership state event from the roomID for observedUserID, download the media assigned
// in the avatar of the membership event, and compare the bytes to see that they are identical to the original profile.
// Return the mxcUri of the membership's avatar
func MustSeeMembershipAvatarAndMatchOriginal(t *testing.T, userLooking client.CSAPI, observedUser client.CSAPI, roomID string, originalMediaBytes []byte) string {
	// t.Helper()
	// observedUser should already have a profile avatar. The membership of a given room should have set an avatar based
	// on that global. It will be a "copy", so the mxcUri will be different from the global version, but should be
	// byte-identical.
	// Use that user to retrieve the content of the state event so we can extract the new "copied" mxc uri. The
	// userLooking may or may not actually be joined to the room. Being joined to the room per state is not the point of
	// these tests
	membership_content := observedUser.MustGetStateEventContent(t, roomID, "m.room.member", observedUser.UserID)
	membership_mxcUri := membership_content.Get("avatar_url").Str

	// the userLooking not be allowed to see this media will fail the test
	mxcPayload2, _ := userLooking.DownloadContentAuthenticated(t, membership_mxcUri)

	areEqual := bytes.Equal(originalMediaBytes, mxcPayload2)
	if areEqual != true {
		t.Fatalf("Media is differing and should be identical")
	}

	return membership_mxcUri
}

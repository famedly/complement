package tests

import (
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

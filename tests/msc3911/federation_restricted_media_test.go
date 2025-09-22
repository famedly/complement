package tests

import (
	"bytes"
	"testing"

	"github.com/matrix-org/complement"
	"github.com/matrix-org/complement/client"
	"github.com/matrix-org/complement/helpers"
	"github.com/matrix-org/complement/internal/data"
	"github.com/matrix-org/complement/match"
	"github.com/matrix-org/complement/must"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

// This test series is for checking behavior of MSC3911 - Linking Media to Events
// Specifically, these are to test the behavior over federated homeservers

// Note: Most of the functionality of MSC3911 is about being able/unable to retrieve a piece of media referenced
// by a MXC, so a sentinel user will be joined to the room from the remote homeserver. Otherwise, visibility can
// not be properly checked. A piece of media should not be retrievable by a user on a homeserver that did not have
// the event that references it(such as a picture message). I.E. If the picture message with an MXC is not on the
// homeserver to send to it's client, then how would the client ever possibly be expected to be able to view the media?
// The sentinel user guarantees that at the very least, the event involved will be on both servers before the check can
// take place.

func TestFederationRestrictedMediaUnstable(t *testing.T) {
	deployment := complement.Deploy(t, 2)
	defer deployment.Destroy(t)

	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{
		LocalpartSuffix: "alice",
		Password:        "password",
	})
	bob := deployment.Register(t, "hs2", helpers.RegistrationOpts{
		LocalpartSuffix: "bob",
		Password:        "password",
	})
	charlie := deployment.Register(t, "hs2", helpers.RegistrationOpts{
		LocalpartSuffix: "charlie",
		Password:        "password",
	})

	alice.LoginUser(t, alice.UserID, "password")

	// Let's give alice a profile avatar
	aliceGlobalProfileAvatarMxcUri := alice.MustUploadContentRestricted(t, data.MatrixSvg, "alice_avatar.svg", "img/svg")
	alice.MustSetProfileAvatar(t, aliceGlobalProfileAvatarMxcUri)

	// Test that the profile that was just set is viewable and not restricted. Save the bytes payload for later testing
	aliceOriginalProfileBytes, _ := alice.DownloadContentAuthenticated(t, aliceGlobalProfileAvatarMxcUri)
	bob.LoginUser(t, bob.UserID, "password")
	charlie.LoginUser(t, charlie.UserID, "password")

	// The first four test check that a message event, such as an image, is restricted correctly in different room history
	// visibility scenarios

	// Test media uploaded to a "joined" history visibility room is only viewable by the room's joined participants
	t.Run("TestJoinedVisibilityMediaMessage", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "joined")
		MustJoinSentinelToRoom(t, deployment, alice, roomID)

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(bob.UserID, roomID))

		// Make sure alice see that bob has joined, otherwise the async nature of federation cause the room join to race with the
		// picture message being sent and it will not be viewable properly by bob.
		alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(bob.UserID, roomID))

		mxcUri, message_event_id := MustUploadMediaAttachToMessageEventAndSendIntoRoom(t, alice, roomID)

		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHasEventID(roomID, message_event_id))

		// bob should have the right to see that media
		bob.DownloadContentAuthenticated(t, mxcUri)

		// charlie is a little trouble maker and found alice's picture mxc uri
		// Since charlie can not see into the room, the media is not downloadable
		response := charlie.UncheckedDownloadContentAuthenticated(t, mxcUri)
		must.MatchResponse(t, response, match.HTTPResponse{StatusCode: 403})

	})

	// Test media uploaded to a "invited" history visibility room is only viewable by the room's invited participants
	t.Run("TestInvitedVisibilityMediaMessage", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "invited")
		sentinel := MustJoinSentinelToRoom(t, deployment, alice, roomID)

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		// Notice that bob is not joined to this room

		mxcUri, message_event_id := MustUploadMediaAttachToMessageEventAndSendIntoRoom(t, alice, roomID)

		// Use the sentinel user to make sure the message has federated before continuing
		sentinel.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHasEventID(roomID, message_event_id))

		// bob should have the right to see that media.
		bob.DownloadContentAuthenticated(t, mxcUri)

		// charlie is a little trouble maker and found alice's picture mxc uri
		// Since charlie can not see into the room, the media is not downloadable
		response := charlie.UncheckedDownloadContentAuthenticated(t, mxcUri)
		must.MatchResponse(t, response, match.HTTPResponse{StatusCode: 403})
	})

	// Test media uploaded to a "shared" history visibility room is viewable by any one(this is wrong, but how it is with Synapse)
	t.Run("TestSharedVisibilityMediaMessage", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "shared")
		MustJoinSentinelToRoom(t, deployment, alice, roomID)

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

		alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(bob.UserID, roomID))

		mxcUri, message_event_id := MustUploadMediaAttachToMessageEventAndSendIntoRoom(t, alice, roomID)

		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHasEventID(roomID, message_event_id))

		// bob should have the right to see that media
		bob.DownloadContentAuthenticated(t, mxcUri)

		// because of the shared visibility being incorrect in synapse, this is viewable
		charlie.DownloadContentAuthenticated(t, mxcUri)
	})

	// Test media uploaded to a "world_viewable" history visibility room is viewable by any one
	t.Run("TestWorldReadableVisibilityMediaMessage", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "world_readable")
		MustJoinSentinelToRoom(t, deployment, alice, roomID)

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

		alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(bob.UserID, roomID))

		mxcUri, message_event_id := MustUploadMediaAttachToMessageEventAndSendIntoRoom(t, alice, roomID)

		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHasEventID(roomID, message_event_id))

		// This is a world_readable room, everyone can see this media
		bob.DownloadContentAuthenticated(t, mxcUri)
		charlie.DownloadContentAuthenticated(t, mxcUri)
	})

	// The MembershipAvatar series tests that media restricted to a membership state event is viewable.

	// Test profile avatar for membership event is only viewable by "joined" room participants
	t.Run("TestJoinedVisibilityMembershipAvatar", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "joined")
		MustJoinSentinelToRoom(t, deployment, alice, roomID)

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

		// Make sure alice see that bob has joined, otherwise the async nature of federation cause the room join to race with the
		// picture message being sent and it will not be viewable properly by bob.
		alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(bob.UserID, roomID))

		mxcUri, membership_event_id := MustUploadMediaAttachToMembershipEventAndSendIntoRoom(t, alice, roomID)

		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHasEventID(roomID, membership_event_id))

		// bob should have the right to see that media
		bob.DownloadContentAuthenticated(t, mxcUri)

		// charlie is a little trouble maker and found alice's picture mxc uri
		// Since charlie can not see into the room, the media is not downloadable
		response := charlie.UncheckedDownloadContentAuthenticated(t, mxcUri)
		must.MatchResponse(t, response, match.HTTPResponse{StatusCode: 403})
	})

	// Test profile avatar for membership event is only viewable by invited room participants
	t.Run("TestInvitedVisibilityMembershipAvatar", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "invited")
		sentinel := MustJoinSentinelToRoom(t, deployment, alice, roomID)

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		// Notice that bob is not joined to the room

		mxcUri, message_event_id := MustUploadMediaAttachToMembershipEventAndSendIntoRoom(t, alice, roomID)

		// Use the sentinel user to make sure the event has federated before continuing
		sentinel.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHasEventID(roomID, message_event_id))

		// bob should have the right to see that media.
		bob.DownloadContentAuthenticated(t, mxcUri)

		// charlie is a little trouble maker and found alice's picture mxc uri
		// Since charlie can not see into the room, the media is not downloadable
		response := charlie.UncheckedDownloadContentAuthenticated(t, mxcUri)
		must.MatchResponse(t, response, match.HTTPResponse{StatusCode: 403})
	})

	// Test profile avatar for membership event is viewable by room participants
	t.Run("TestSharedVisibilityMembershipAvatar", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "shared")
		MustJoinSentinelToRoom(t, deployment, alice, roomID)

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

		alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(bob.UserID, roomID))

		mxcUri, membership_event_id := MustUploadMediaAttachToMembershipEventAndSendIntoRoom(t, alice, roomID)

		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHasEventID(roomID, membership_event_id))

		// bob should have the right to see that media
		bob.DownloadContentAuthenticated(t, mxcUri)

		// because of the shared visibility being incorrect in synapse, this is viewable
		charlie.DownloadContentAuthenticated(t, mxcUri)
	})

	// Test profile avatar for membership event is viewable by room participants
	t.Run("TestWorldReadableVisibilityMembershipAvatar", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "world_readable")
		MustJoinSentinelToRoom(t, deployment, alice, roomID)

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

		alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(bob.UserID, roomID))

		mxcUri, membership_event_id := MustUploadMediaAttachToMembershipEventAndSendIntoRoom(t, alice, roomID)

		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHasEventID(roomID, membership_event_id))

		// This is a world_readable room, everyone can see this media
		bob.DownloadContentAuthenticated(t, mxcUri)
		charlie.DownloadContentAuthenticated(t, mxcUri)
	})

	// Test global profile avatar is copied for initial membership event in a room. This will end up being viewable by anyone(thanks
	// to Synapse shared view bug), that is not what is being tested here, so use the most restricive history visibility.
	// Note:  the global profile that was established on login is the SVG file and our membership state tests above use the PNG file.
	t.Run("TestRoomCreationMembershipAvatarIsACopy", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "joined")
		// Retrieve alice's membership event so we can pry the avatar_url out of it
		aliceMemberEvent := alice.MustGetStateEventContent(t, roomID, "m.room.member", alice.UserID)

		avatarUrl := aliceMemberEvent.Get("avatar_url").Str

		// make sure it is different than the global profile mxc
		if avatarUrl == aliceGlobalProfileAvatarMxcUri {
			t.Fatalf("mxcUri values should not match for global profile and room membership event")
		}

		// download it and check that the bytes match
		aliceNewAvatarBytes, _ := alice.DownloadContentAuthenticated(t, avatarUrl)
		if !bytes.Equal(aliceOriginalProfileBytes, aliceNewAvatarBytes) {
			t.Fatalf("Media is differing and should be identical")
		}

	})

}

// Tank has one job, be part of the room so that federation works. No other interaction except running sync to make
// sure an event has arrived.
func MustJoinSentinelToRoom(t *testing.T, deployment complement.Deployment, activeUser *client.CSAPI, roomID string) *client.CSAPI {
	sentinelUser := deployment.Register(t, "hs2", helpers.RegistrationOpts{
		LocalpartSuffix: "Tank",
		Password:        "password",
	})
	sentinelUser.LoginUser(t, sentinelUser.UserID, "password")

	activeUser.MustInviteRoom(t, roomID, sentinelUser.UserID)
	sentinelUser.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(sentinelUser.UserID, roomID))

	sentinelUser.MustJoinRoom(t, roomID, []spec.ServerName{
		deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
	})

	return sentinelUser

}

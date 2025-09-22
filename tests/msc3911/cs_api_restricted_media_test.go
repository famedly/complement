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

func TestRestrictedMediaUnstable(t *testing.T) {
	deployment := complement.Deploy(t, 2)
	defer deployment.Destroy(t)

	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{
		LocalpartSuffix: "alice",
		Password:        "password",
	})
	bob := deployment.Register(t, "hs1", helpers.RegistrationOpts{
		LocalpartSuffix: "bob",
		Password:        "password",
	})
	charlie := deployment.Register(t, "hs1", helpers.RegistrationOpts{
		LocalpartSuffix: "charlie",
		Password:        "password",
	})

	alice.LoginUser(t, alice.UserID, "password")

	// Let's give alice a profile avatar
	aliceGlobalProfileAvatarMxcUri := alice.MustUploadContentRestricted(t, data.MatrixSvg, "alice_avatar.svg", "img/svg")
	alice.MustSetProfileAvatar(t, aliceGlobalProfileAvatarMxcUri)

	// Test that the profile that was just set is viewable and not restricted
	aliceOriginalProfileBytes, _ := alice.DownloadContentAuthenticated(t, aliceGlobalProfileAvatarMxcUri)
	bob.LoginUser(t, bob.UserID, "password")
	charlie.LoginUser(t, charlie.UserID, "password")

	// The first four test check that a message event, such as an image, is restricted correctly in different room history
	// visibility scenarios

	// Test media uploaded to a "joined" history visibility room is only viewable by the room's joined participants
	t.Run("TestJoinedVisibilityMediaMessage", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "joined")

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

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

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))

		mxcUri, _ := MustUploadMediaAttachToMessageEventAndSendIntoRoom(t, alice, roomID)

		// bob should have the right to see that media
		bob.DownloadContentAuthenticated(t, mxcUri)

		// charlie is a little trouble maker and found alice's picture mxc uri
		// Since charlie can not see into the room, the media is not downloadable
		response := charlie.UncheckedDownloadContentAuthenticated(t, mxcUri)
		must.MatchResponse(t, response, match.HTTPResponse{StatusCode: 403})

	})

	// Test media uploaded to a "shared" history visibility room is viewable by any one(this is wrong, but how it is with Synapse)
	t.Run("TestSharedVisibilityMediaMessage", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "shared")

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

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

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

		mxcUri, message_event_id := MustUploadMediaAttachToMessageEventAndSendIntoRoom(t, alice, roomID)

		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncTimelineHasEventID(roomID, message_event_id))

		// This is a world_readable room, everyone can see this media
		bob.DownloadContentAuthenticated(t, mxcUri)
		charlie.DownloadContentAuthenticated(t, mxcUri)
	})

	// The MembershipAvatar series tests that media restricted to a membership state event is viewable.

	// Test profile avatar for membership event is only viewable by joined room participants
	t.Run("TestJoinedVisibilityMembershipAvatar", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "joined")

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

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

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		// Notice that bob is not joined to the room

		mxcUri, _ := MustUploadMediaAttachToMembershipEventAndSendIntoRoom(t, alice, roomID)

		// bob should have the right to see that media. Not sure how he got the mxc without being in the room, but it is allowed.
		bob.DownloadContentAuthenticated(t, mxcUri)

		// charlie is a little trouble maker and found alice's picture mxc uri
		// Since charlie can not see into the room, the media is not downloadable
		response := charlie.UncheckedDownloadContentAuthenticated(t, mxcUri)
		must.MatchResponse(t, response, match.HTTPResponse{StatusCode: 403})
	})

	// Test profile avatar for membership event is viewable by room participants
	t.Run("TestSharedVisibilityMembershipAvatar", func(t *testing.T) {
		roomID := MustCreateRoomWithHistoryVisibility(t, alice, "shared")

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

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

		alice.MustInviteRoom(t, roomID, bob.UserID)
		bob.MustSyncUntil(t, client.SyncReq{}, client.SyncInvitedTo(bob.UserID, roomID))
		bob.MustJoinRoom(t, roomID, []spec.ServerName{
			deployment.GetFullyQualifiedHomeserverName(t, "hs1"),
		})

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

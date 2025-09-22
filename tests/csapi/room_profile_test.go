package csapi_tests

import (
	"testing"

	"github.com/tidwall/gjson"

	"github.com/matrix-org/complement"
	"github.com/matrix-org/complement/client"
	"github.com/matrix-org/complement/helpers"
)

func TestAvatarUrlUpdate(t *testing.T) {
	testProfileFieldUpdate(t, "avatar_url", "mxc://example.com/LemurLover")
}

func TestDisplayNameUpdate(t *testing.T) {
	testProfileFieldUpdate(t, "displayname", "LemurLover")
}

// sytest: $datum updates affect room member events
func testProfileFieldUpdate(t *testing.T, field string, bogusData string) {
	deployment := complement.Deploy(t, 1)
	defer deployment.Destroy(t)

	alice := deployment.Register(t, "hs1", helpers.RegistrationOpts{})

	roomID := alice.MustCreateRoom(t, map[string]interface{}{
		"preset": "public_chat",
	})

	sinceToken := alice.MustSyncUntil(t, client.SyncReq{}, client.SyncJoinedTo(alice.UserID, roomID))

	alice.MustDo(
		t,
		"PUT",
		[]string{"_matrix", "client", "v3", "profile", alice.UserID, field},
		client.WithJSONBody(t, map[string]interface{}{
			field: bogusData,
		}),
	)

	alice.MustSyncUntil(t, client.SyncReq{Since: sinceToken}, client.SyncJoinedTo(alice.UserID, roomID, func(result gjson.Result) bool {
		return result.Get("content."+field).Str == bogusData
	}))
}

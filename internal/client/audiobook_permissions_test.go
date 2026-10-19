package client

import "testing"

func TestHasPermissionForAudiobookLevels(t *testing.T) {
	cases := []struct {
		name        string
		permissions authPermissions
		minimum     permissionLevel
		want        bool
	}{
		{"no access cannot view", authPermissions{}, permissionViewAudiobooks, false},
		{"no access cannot upload", authPermissions{}, permissionUploadAudiobooks, false},
		{"view-only can view", authPermissions{CanViewAudiobooks: true}, permissionViewAudiobooks, true},
		{"view-only cannot upload", authPermissions{CanViewAudiobooks: true}, permissionUploadAudiobooks, false},
		{"upload implies view", authPermissions{CanUploadAudiobooks: true}, permissionViewAudiobooks, true},
		{"upload can upload", authPermissions{CanUploadAudiobooks: true}, permissionUploadAudiobooks, true},
		{"manage users implies view", authPermissions{CanManageUsers: true}, permissionViewAudiobooks, true},
		{"manage users implies upload", authPermissions{CanManageUsers: true}, permissionUploadAudiobooks, true},
		{"meme view/upload do not grant audiobook access", authPermissions{CanView: true, CanUpload: true}, permissionViewAudiobooks, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasPermission(tc.permissions, tc.minimum); got != tc.want {
				t.Errorf("hasPermission(%+v, %q) = %v, want %v", tc.permissions, tc.minimum, got, tc.want)
			}
		})
	}
}

func TestApplyPermissionPolicyDerivesAudiobookView(t *testing.T) {
	auth := &authService{config: DiscordAuthConfig{SuperAdminUserIDs: map[string]struct{}{"super": {}}}}

	derived := auth.applyPermissionPolicy("regular", authPermissions{CanUploadAudiobooks: true})
	if !derived.CanViewAudiobooks {
		t.Error("uploading audiobooks should imply view access")
	}

	superAdmin := auth.applyPermissionPolicy("super", authPermissions{})
	if !superAdmin.CanViewAudiobooks || !superAdmin.CanUploadAudiobooks {
		t.Error("super admins should have full audiobook access")
	}
}

// Copyright 2021 Gravitational, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package webauthncli_test

import (
	"context"
	"testing"
	"time"

	"github.com/duo-labs/webauthn/protocol"
	"github.com/duo-labs/webauthn/protocol/webauthncose"
	"github.com/gravitational/teleport/api/types"
	"github.com/stretchr/testify/require"

	wanlib "github.com/gravitational/teleport/lib/auth/webauthn"
	wancli "github.com/gravitational/teleport/lib/auth/webauthncli"
)

func TestRegister(t *testing.T) {
	resetU2FCallbacksAfterTest(t)

	const user = "llama"
	const rpID = "example.com"
	const origin = "https://example.com"

	// Prepare a few fake devices.
	u2fKey, err := newFakeDevice("u2fkey" /* name */, "" /* appID */)
	require.NoError(t, err)
	registeredKey, err := newFakeDevice("regkey" /* name */, rpID /* appID */)
	require.NoError(t, err)

	// Create a registration flow, we'll use it to both generate credential
	// requests and to validate them.
	webRegistration := &wanlib.RegistrationFlow{
		Webauthn: &types.Webauthn{
			RPID: rpID,
		},
		Identity: &fakeIdentity{
			Devices: []*types.MFADevice{registeredKey.mfaDevice},
		},
	}

	ctx := context.Background()
	tests := []struct {
		name            string
		devs            []*fakeDevice
		setUserPresence *fakeDevice
		wantErr         bool
		wantRawID       []byte
	}{
		{
			name:            "U2F-compatible registration",
			devs:            []*fakeDevice{u2fKey},
			setUserPresence: u2fKey,
			wantRawID:       u2fKey.key.KeyHandle,
		},
		{
			name:            "Registered key ignored",
			devs:            []*fakeDevice{u2fKey, registeredKey},
			setUserPresence: registeredKey,
			wantErr:         true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// 100ms is an eternity when probing devices at 1ns intervals.
			ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()

			cc, err := webRegistration.Begin(ctx, user)
			require.NoError(t, err)

			// Reset/set presence indicator.
			for _, dev := range test.devs {
				dev.SetUserPresence(false)
			}
			test.setUserPresence.SetUserPresence(true)

			// Replace U2F library functions with our mocked versions.
			fakeDevs := &fakeDevices{devs: test.devs}
			fakeDevs.assignU2FCallbacks()

			resp, err := wancli.Register(ctx, origin, cc)
			switch hasErr := err != nil; {
			case hasErr != test.wantErr:
				t.Fatalf("Register returned err = %v, wantErr = %v", err, test.wantErr)
			case hasErr: // OK.
				return
			}
			require.Equal(t, test.wantRawID, resp.GetWebauthn().RawId)

			_, err = webRegistration.Finish(ctx, user, u2fKey.name, wanlib.CredentialCreationResponseFromProto(resp.GetWebauthn()))
			require.NoError(t, err, "server-side registration failed")
		})
	}
}

func TestRegister_errors(t *testing.T) {
	ctx := context.Background()

	const origin = "https://example.com"
	webRegistration := &wanlib.RegistrationFlow{
		Webauthn: &types.Webauthn{
			RPID: "example.com",
		},
		Identity: &fakeIdentity{},
	}
	okCC, err := webRegistration.Begin(ctx, "llama" /* user */)
	require.NoError(t, err)

	tests := []struct {
		name    string
		origin  string
		makeCC  func() *wanlib.CredentialCreation
		wantErr string
	}{
		{
			name:    "NOK empty origin",
			origin:  "",
			makeCC:  func() *wanlib.CredentialCreation { return okCC },
			wantErr: "origin",
		},
		{
			name:    "NOK nil credential creation",
			origin:  origin,
			makeCC:  func() *wanlib.CredentialCreation { return nil },
			wantErr: "credential creation required",
		},
		{
			name:    "NOK nil empty creation",
			origin:  origin,
			makeCC:  func() *wanlib.CredentialCreation { return &wanlib.CredentialCreation{} },
			wantErr: "relying party",
		},
		{
			name:   "NOK ES256 algorithm not allowed",
			origin: origin,
			makeCC: func() *wanlib.CredentialCreation {
				cp := *okCC
				var params []protocol.CredentialParameter
				for _, p := range cp.Response.Parameters {
					if p.Algorithm == webauthncose.AlgES256 {
						continue
					}
					params = append(params, p)
				}
				cp.Response.Parameters = params
				return &cp
			},
			wantErr: "ES256 not allowed",
		},
		{
			name:   "NOK platform attachment required",
			origin: origin,
			makeCC: func() *wanlib.CredentialCreation {
				cp := *okCC
				cp.Response.AuthenticatorSelection.AuthenticatorAttachment = protocol.Platform
				return &cp
			},
			wantErr: "platform",
		},
		{
			name:   "NOK resident key required",
			origin: origin,
			makeCC: func() *wanlib.CredentialCreation {
				cp := *okCC
				rrk := true
				cp.Response.AuthenticatorSelection.RequireResidentKey = &rrk
				return &cp
			},
			wantErr: "resident key",
		},
		{
			name:   "NOK user verification required",
			origin: origin,
			makeCC: func() *wanlib.CredentialCreation {
				cp := *okCC
				cp.Response.AuthenticatorSelection.UserVerification = protocol.VerificationRequired
				return &cp
			},
			wantErr: "user verification",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := wancli.Register(ctx, test.origin, test.makeCC())
			require.Error(t, err)
			require.Contains(t, err.Error(), test.wantErr)
		})
	}
}

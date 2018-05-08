/*
 * Copyright © 2015-2018 Aeneas Rekkas <aeneas+oss@aeneas.io>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * @author		Aeneas Rekkas <aeneas+oss@aeneas.io>
 * @copyright 	2015-2018 Aeneas Rekkas <aeneas+oss@aeneas.io>
 * @license 	Apache-2.0
 */

package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/ory/go-convenience/urlx"
	"github.com/ory/hydra/pkg"
	"github.com/ory/hydra/rand/sequence"
	"github.com/spf13/cobra"
	"github.com/toqueteos/webbrowser"
	"golang.org/x/oauth2"
)

// tokenUserCmd represents the token command
var tokenUserCmd = &cobra.Command{
	Use:   "user",
	Short: "Generate an OAuth2 token using the code flow",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		if ok, _ := cmd.Flags().GetBool("skip-tls-verify"); ok {
			// fmt.Println("Warning: Skipping TLS Certificate Verification.")
			ctx = context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}})
		}

		scopes, _ := cmd.Flags().GetStringSlice("scope")
		redirectUrl, _ := cmd.Flags().GetString("redirect")
		backend, _ := cmd.Flags().GetString("token-url")
		frontend, _ := cmd.Flags().GetString("auth-url")

		clientID, _ := cmd.Flags().GetString("client-id")
		clientSecret, _ := cmd.Flags().GetString("client-secret")
		if clientID == "" || clientSecret == "" {
			fmt.Print(cmd.UsageString())
			fmt.Println("Please provide a Client ID and Client Secret using flags --client-id and --client-secret, or environment variables OAUTH2_CLIENT_ID and OAUTH2_CLIENT_SECRET.")
			return
		}

		if backend == "" {
			bu, err := url.Parse(c.GetClusterURLWithoutTailingSlash(cmd))
			pkg.Must(err, `Unable to parse cluster url ("%s"): %s`, c.GetClusterURLWithoutTailingSlash(cmd), err)
			backend = urlx.AppendPaths(bu, "/oauth2/token").String()
		}
		if frontend == "" {
			fu, err := url.Parse(c.GetClusterURLWithoutTailingSlash(cmd))
			pkg.Must(err, `Unable to parse cluster url ("%s"): %s`, c.GetClusterURLWithoutTailingSlash(cmd), err)
			frontend = urlx.AppendPaths(fu, "/oauth2/auth").String()
		}

		conf := oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint: oauth2.Endpoint{
				TokenURL: backend,
				AuthURL:  frontend,
			},
			RedirectURL: redirectUrl,
			Scopes:      scopes,
		}

		state, err := sequence.RuneSequence(24, sequence.AlphaLower)
		pkg.Must(err, "Could not generate random state: %s", err)

		nonce, err := sequence.RuneSequence(24, sequence.AlphaLower)
		pkg.Must(err, "Could not generate random state: %s", err)

		location := conf.AuthCodeURL(string(state)) + "&nonce=" + string(nonce)

		if ok, _ := cmd.Flags().GetBool("no-open"); !ok {
			webbrowser.Open(location)
		}

		fmt.Println("Setting up callback listener on http://localhost:4445/callback")
		fmt.Println("Press ctrl + c on Linux / Windows or cmd + c on OSX to end the process.")
		fmt.Printf("If your browser does not open automatically, navigate to:\n\n\t%s\n\n", location)

		r := httprouter.New()
		server := &http.Server{Addr: ":4445", Handler: r}
		var shutdown = func() {
			time.Sleep(time.Second * 1)
			ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
			server.Shutdown(ctx)
		}

		r.GET("/callback", func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
			if len(r.URL.Query().Get("error")) > 0 {
				fmt.Printf("Got error: %s\n", r.URL.Query().Get("error_description"))

				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "<html><body><h1>An error occurred</h1><h2>%s</h2><p>%s</p></body></html>", r.URL.Query().Get("error"), r.URL.Query().Get("error_description"))
				go shutdown()
				return
			}

			if r.URL.Query().Get("state") != string(state) {
				fmt.Printf("States do not match. Expected %s, got %s\n", string(state), r.URL.Query().Get("state"))

				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "<html><body><h1>An error occurred</h1><h2>%s</h2><p>%s</p></body></html>", "States do not match", "Expected state "+string(state)+" but got "+r.URL.Query().Get("state"))
				go shutdown()
				return
			}

			code := r.URL.Query().Get("code")
			token, err := conf.Exchange(ctx, code)
			pkg.Must(err, "Could not exchange code for token: %s", err)

			fmt.Printf("Access Token:\n\t%s\n", token.AccessToken)
			fmt.Printf("Refresh Token:\n\t%s\n\n", token.RefreshToken)
			fmt.Printf("Expires in:\n\t%s\n\n", token.Expiry)

			w.Write([]byte(fmt.Sprintf(`
<html><head></head><body>
<ul>
	<li>Access Token: <code>%s</code></li>
	<li>Refresh Token: <code>%s</code></li>
	<li>Expires in: <code>%s</code></li>
`, token.AccessToken, token.RefreshToken, token.Expiry)))

			idt := token.Extra("id_token")
			if idt != nil {
				w.Write([]byte(fmt.Sprintf(`<li>ID Token: <code>%s</code></li>`, idt)))
				fmt.Printf("ID Token:\n\t%s\n\n", idt)
			}
			w.Write([]byte("</ul></body></html>"))

			go shutdown()
		})
		server.ListenAndServe()
	},
}

func init() {
	tokenCmd.AddCommand(tokenUserCmd)
	tokenUserCmd.Flags().Bool("no-open", false, "Do not open the browser window automatically")
	tokenUserCmd.Flags().StringSlice("scope", []string{"offline", "openid"}, "Request OAuth2 scope")

	tokenUserCmd.Flags().String("client-id", os.Getenv("OAUTH2_CLIENT_ID"), "Use the provided OAuth 2.0 Client ID, defaults to environment variable OAUTH2_CLIENT_ID")
	tokenUserCmd.Flags().String("client-secret", os.Getenv("OAUTH2_CLIENT_SECRET"), "Use the provided OAuth 2.0 Client Secret, defaults to environment variable OAUTH2_CLIENT_SECRET")

	tokenUserCmd.Flags().String("redirect", "http://localhost:4445/callback", "Force a redirect url")
	tokenUserCmd.Flags().String("auth-url", os.Getenv("HYDRA_URL"), "Usually it is enough to specify the `endpoint` flag, but if you want to force the authorization url, use this flag")
	tokenUserCmd.Flags().String("token-url", os.Getenv("HYDRA_URL"), "Usually it is enough to specify the `endpoint` flag, but if you want to force the token url, use this flag")
	tokenUserCmd.PersistentFlags().String("endpoint", os.Getenv("HYDRA_URL"), "Set the URL where ORY Hydra is hosted, defaults to environment variable HYDRA_URL")
}

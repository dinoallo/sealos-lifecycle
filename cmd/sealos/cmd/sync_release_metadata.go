// Copyright © 2026 sealos.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/labring/sealos/pkg/distribution/bom"
)

func newSyncReleaseMetadataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release-metadata",
		Short: "Serve release channel and BOM metadata for channel lookup",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSyncReleaseMetadataServeCmd())
	return cmd
}

func newSyncReleaseMetadataServeCmd() *cobra.Command {
	var flags struct {
		releaseSource string
		listen        string
	}

	cmd := &cobra.Command{
		Use:          "serve",
		Short:        "Serve a local release metadata directory through the lookup API",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			handler, err := bom.NewReleaseMetadataHandler(flags.releaseSource)
			if err != nil {
				return err
			}
			server := &http.Server{
				Addr:              strings.TrimSpace(flags.listen),
				Handler:           handler,
				ReadHeaderTimeout: 10 * time.Second,
			}
			go func() {
				<-cmd.Context().Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
			}()
			fmt.Fprintf(cmd.OutOrStdout(), "release metadata service listening on http://%s\n", server.Addr)
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.releaseSource, "release-source", "", "local release metadata directory to serve")
	cmd.Flags().StringVar(&flags.listen, "listen", "127.0.0.1:8080", "address for the release metadata service")
	if err := cmd.MarkFlagRequired("release-source"); err != nil {
		panic(err)
	}
	return cmd
}

/*
Copyright © 2020 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/iqlusioninc/relayer/relayer"
	"github.com/spf13/cobra"
	"log"
	"strings"
	"sync"
	"time"
)

// transactionCmd represents the tx command
func transactionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "transact",
		Aliases: []string{"tx"},
		Short:   "IBC Transaction Commands",
		Long: strings.TrimSpace(`Commands to create IBC transactions on configured chains. Most of these commands take a '[path]' arguement. Make sure:
	1. Chains are properly configured to relay over by using the 'rly chains list' command
	2. Path is properly configured to relay over by using the 'rly paths list' command`),
	}

	cmd.AddCommand(
		fullPathCmd(),
		relayMsgsCmd(),
		transferCmd(),
		flags.LineBreak,
		createClientsCmd(),
		createConnectionCmd(),
		createChannelCmd(),
		closeChannelCmd(),
		flags.LineBreak,
		rawTransactionCmd(),
		sendPacketCmd(),
		multiRelayMsgsCmd(),
	)

	return cmd
}

func createClientsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "clients [path-name]",
		Aliases: []string{"clnts"},
		Short:   "create a clients between two configured chains with a configured path",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, src, dst, err := config.ChainsFromPath(args[0])
			if err != nil {
				return err
			}

			return c[src].CreateClients(c[dst])
		},
	}
	return cmd
}

func createConnectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "connection [path-name]",
		Aliases: []string{"conn"},
		Short:   "create a connection between two configured chains with a configured path",
		Long:    "This command is meant to be used to repair or create a connection between two chains with a configured path in the config file",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, src, dst, err := config.ChainsFromPath(args[0])
			if err != nil {
				return err
			}

			to, err := getTimeout(cmd)
			if err != nil {
				return err
			}

			return c[src].CreateConnection(c[dst], to)
		},
	}

	return timeoutFlag(cmd)
}

func createChannelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "channel [path-name]",
		Aliases: []string{"chan"},
		Short:   "create a channel between two configured chains with a configured path",
		Long:    "This command is meant to be used to repair or create a channel between two chains with a configured path in the config file",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, src, dst, err := config.ChainsFromPath(args[0])
			if err != nil {
				return err
			}

			to, err := getTimeout(cmd)
			if err != nil {
				return err
			}

			// TODO: read order out of path config
			return c[src].CreateChannel(c[dst], true, to)
		},
	}

	return timeoutFlag(cmd)
}

func closeChannelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "channel-close [path-name]",
		Aliases: []string{"chan-cl", "close", "cl"},
		Short:   "close a channel between two configured chains with a configured path",
		Long:    "This command is meant to close a channel",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, src, dst, err := config.ChainsFromPath(args[0])
			if err != nil {
				return err
			}

			to, err := getTimeout(cmd)
			if err != nil {
				return err
			}

			return c[src].CloseChannel(c[dst], to)
		},
	}

	return timeoutFlag(cmd)
}

func fullPathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "link [path-name]",
		Aliases: []string{"full-path", "connect", "path", "pth"},
		Short:   "create clients, connection, and channel between two configured chains with a configured path",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, src, dst, err := config.ChainsFromPath(args[0])
			if err != nil {
				return err
			}

			to, err := getTimeout(cmd)
			if err != nil {
				return err
			}

			if err = c[src].CreateClients(c[dst]); err != nil {
				return err
			}

			if err = c[src].CreateConnection(c[dst], to); err != nil {
				return err
			}

			return c[src].CreateChannel(c[dst], true, to)
		},
	}

	return timeoutFlag(cmd)
}

func relayMsgsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "relay [path-name]",
		Aliases: []string{"rly", "queue"},
		Short:   "relay any packets that remain to be relayed on a given path, in both directions",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, src, dst, err := config.ChainsFromPath(args[0])
			if err != nil {
				return err
			}

			maxPacketLength, err := cmd.Flags().GetUint64(flagMaxMsgLength)
			if err != nil {
				return err
			}

			maxPacketLength = maxPacketLength - 1

			sh, err := relayer.NewSyncHeaders(c[src], c[dst])
			if err != nil {
				return err
			}

			sp, err := relayer.UnrelayedSequences(c[src], c[dst], sh)
			if err != nil {
				return err
			}

			if len(sp.Src) > int(maxPacketLength) {
				sp.Src = sp.Src[:maxPacketLength]
			}
			if len(sp.Dst) > int(maxPacketLength) {
				sp.Dst = sp.Dst[:maxPacketLength]
			}

			path := config.Paths.MustGet(args[0])
			strategy, err := GetStrategyWithOptions(cmd, path.MustGetStrategy())
			if err != nil {
				return err
			}

			return strategy.RelayPacketsOrderedChan(c[src], c[dst], sp, sh)
		},
	}

	return strategyFlag(cmd)
}

func multiRelayMsgsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "multi-relay [src] [dst] [path-names]",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, err := cmd.Flags().GetUint64("limit")
			if err != nil {
				return err
			}

			paths := make(relayer.Paths)
			for _, name := range strings.Split(args[2], ",") {
				paths[name] = config.Paths.MustGet(name)
			}

			maxPacketLength, err := cmd.Flags().GetUint64(flagMaxMsgLength)
			if err != nil {
				return err
			}

			maxPacketLength = maxPacketLength - 1

			for name, path := range paths {
				if path.Src.ChainID != args[0] || path.Dst.ChainID != args[1] {
					return fmt.Errorf("source or destination chain id does not match in provided path %s", name)
				}
			}

			increase := make(chan bool)
			src := config.Chains.MustGet(args[0])
			dst := config.Chains.MustGet(args[1]).
				WithIncreaseSequenceChan(increase)

			for ; limit > 0; limit-- {
				start := time.Now()
				log.Printf("[INI] {%s}\n", start)

				account, err := dst.GetAccount()
				if err != nil {
					return err
				}

				sequence := account.GetSequence()
				dst.WithAccountNumber(account.GetAccountNumber()).
					WithSequence(sequence)

				var wg sync.WaitGroup
				for name, path := range paths {
					wg.Add(1)
					go func(name string) {
						log.Printf("[STA] {%s | %d}\n", name, sequence)

						var (
							headers  *relayer.SyncHeaders
							relay    *relayer.RelaySequences
							strategy relayer.Strategy
							err      error
						)

						defer func() {
							if err != nil {
								log.Printf("[ERR] {%s} (%s)\n", name, err)
								increase <- false
							}

							log.Printf("[END] {%s}\n", name)
							wg.Done()
						}()

						if err = src.SetPath(path.Src); err != nil {
							return
						}
						if err = dst.SetPath(path.Dst); err != nil {
							return
						}

						headers, err = relayer.NewSyncHeaders(src, dst)
						if err != nil {
							return
						}

						relay, err = relayer.UnrelayedSequences(src, dst, headers)
						if err != nil {
							return
						}

						log.Printf("[NOP] {%d | %d}", len(relay.Src), len(relay.Dst))

						if len(relay.Src) > int(maxPacketLength) {
							relay.Src = relay.Src[:maxPacketLength]
						}
						if len(relay.Dst) > int(maxPacketLength) {
							relay.Dst = relay.Dst[:maxPacketLength]
						}

						if len(relay.Src) == 0 && len(relay.Dst) == 0 {
							err = fmt.Errorf("no packets to relay")
							return
						}

						strategy, err = GetStrategyWithOptions(cmd, path.MustGetStrategy())
						if err != nil {
							return
						}

						err = strategy.RelayPacketsOrderedChan(src, dst, relay, headers)
					}(name)

					log.Print("[WAI] (increase)\n")
					if <-increase {
						sequence++
						dst.WithSequence(sequence)
					}
				}

				log.Printf("[WAI] (work group)\n")
				wg.Wait()

				log.Printf("[DON] {%s}\n", time.Since(start))
			}

			return nil
		},
	}

	cmd.Flags().Uint64("limit", 1, "Max number of runs")

	return strategyFlag(cmd)
}

func sendPacketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "send-packet [src-chain-id] [dst-chain-id] [packet-data]",
		Aliases: []string{"pkt", "sp"},
		Short:   "send a raw packet from a source chain to a destination chain",
		Long:    "This sends packet-data (default: stdin) from a relayer's configured wallet on chain src to chain dst",
		Args:    cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, dst := args[0], args[1]
			c, err := config.Chains.Gets(src, dst)
			if err != nil {
				return err
			}

			pth, err := cmd.Flags().GetString(flagPath)
			if err != nil {
				return err
			}

			if _, err = setPathsFromArgs(c[src], c[dst], pth); err != nil {
				return err
			}

			var packetData string
			if len(args) < 3 {
				// Reading from stdin.
				if _, err := fmt.Scanln(&packetData); err != nil {
					return err
				}
			} else {
				packetData = args[2]
			}

			return c[src].SendPacket(c[dst], []byte(packetData))
		},
	}
	return pathFlag(cmd)
}

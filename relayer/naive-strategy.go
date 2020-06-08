package relayer

import (
	"fmt"
	"strconv"
	"time"

	"github.com/avast/retry-go"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ Strategy = &NaiveStrategy{}

// NewNaiveStrategy returns the proper config for the NaiveStrategy
func NewNaiveStrategy() *StrategyCfg {
	return &StrategyCfg{
		Type: (&NaiveStrategy{}).GetType(),
	}
}

// NaiveStrategy is an implementation of Strategy.
type NaiveStrategy struct {
	Ordered      bool
	MaxTxSize    uint64 // maximum permitted size of the msgs in a bundled relay transaction
	MaxMsgLength uint64 // maximum amount of messages in a bundled relay transaction
}

// GetType implements Strategy
func (nrs *NaiveStrategy) GetType() string {
	return "naive"
}

// UnrelayedSequencesOrdered returns the unrelayed sequence numbers between two chains
func (nrs *NaiveStrategy) UnrelayedSequencesOrdered(src, dst *Chain, sh *SyncHeaders) (*RelaySequences, error) {
	return UnrelayedSequences(src, dst, sh)
}

// UnrelayedSequencesUnordered returns the unrelayed sequence numbers between two chains
func (nrs *NaiveStrategy) UnrelayedSequencesUnordered(src, dst *Chain, sh *SyncHeaders) (*RelaySequences, error) {
	return UnrelayedSequences(src, dst, sh)
}

// HandleEvents defines how the relayer will handle block and transaction events as they are emmited
func (nrs *NaiveStrategy) HandleEvents(src, dst *Chain, sh *SyncHeaders, events map[string][]string) {
	rlyPackets, err := relayPacketsFromEventListener(src.PathEnd, dst.PathEnd, events)
	if len(rlyPackets) > 0 && err == nil {
		nrs.sendTxFromEventPackets(src, dst, rlyPackets, sh)
	}
}

func relayPacketsFromEventListener(src, dst *PathEnd, events map[string][]string) (rlyPkts []relayPacket, err error) {
	// check for send packets
	if pdval, ok := events["send_packet.packet_data"]; ok {
		for i, pd := range pdval {
			// Ensure that we only relay over the channel and port specified
			// OPTIONAL FEATURE: add additional filtering options
			// Example Events - "transfer.amount(sdk.Coin)", "message.sender(sdk.AccAddress)"
			srcChan, srcPort := events["send_packet.packet_src_channel"], events["send_packet.packet_src_port"]
			dstChan, dstPort := events["send_packet.packet_dst_channel"], events["send_packet.packet_dst_port"]

			// NOTE: Src and Dst are switched here
			if dst.PortID == srcPort[i] && dst.ChannelID == srcChan[i] && src.PortID == dstPort[i] && src.ChannelID == dstChan[i] {
				rp := &relayMsgRecvPacket{packetData: []byte(pd)}

				// next, get and parse the sequence
				if sval, ok := events["send_packet.packet_sequence"]; ok {
					seq, err := strconv.ParseUint(sval[i], 10, 64)
					if err != nil {
						return nil, err
					}
					rp.seq = seq
				}

				// finally, get and parse the timeout
				if sval, ok := events["send_packet.packet_timeout_height"]; ok {
					timeout, err := strconv.ParseUint(sval[i], 10, 64)
					if err != nil {
						return nil, err
					}
					rp.timeout = timeout
				}

				// finally, get and parse the timeout
				if sval, ok := events["send_packet.packet_timeout_timestamp"]; ok {
					timeout, err := strconv.ParseUint(sval[i], 10, 64)
					if err != nil {
						return nil, err
					}
					rp.timeoutStamp = timeout
				}

				// queue the packet for return
				rlyPkts = append(rlyPkts, rp)
			}
		}
	}

	// then, check for packet acks
	if pdval, ok := events["recv_packet.packet_data"]; ok {
		for i, pd := range pdval {
			// Ensure that we only relay over the channel and port specified
			// OPTIONAL FEATURE: add additional filtering options
			srcChan, srcPort := events["recv_packet.packet_src_channel"], events["recv_packet.packet_src_port"]
			dstChan, dstPort := events["recv_packet.packet_dst_channel"], events["recv_packet.packet_dst_port"]

			// NOTE: Src and Dst are not switched here
			if src.PortID == srcPort[i] && src.ChannelID == srcChan[i] && dst.PortID == dstPort[i] && dst.ChannelID == dstChan[i] {
				rp := &relayMsgPacketAck{packetData: []byte(pd)}

				// first get the ack
				if ack, ok := events["recv_packet.packet_ack"]; ok {
					rp.ack = []byte(ack[i])
				}
				// next, get and parse the sequence
				if sval, ok := events["recv_packet.packet_sequence"]; ok {
					seq, err := strconv.ParseUint(sval[i], 10, 64)
					if err != nil {
						return nil, err
					}
					rp.seq = seq
				}

				// finally, get and parse the timeout
				if sval, ok := events["recv_packet.packet_timeout_height"]; ok {
					timeout, err := strconv.ParseUint(sval[i], 10, 64)
					if err != nil {
						return nil, err
					}
					rp.timeout = timeout
				}

				// finally, get and parse the timeout
				if sval, ok := events["recv_packet.packet_timeout_timestamp"]; ok {
					timeout, err := strconv.ParseUint(sval[i], 10, 64)
					if err != nil {
						return nil, err
					}
					rp.timeoutStamp = timeout
				}

				// queue the packet for return
				rlyPkts = append(rlyPkts, rp)
			}
		}
	}
	return
}

func (nrs *NaiveStrategy) sendTxFromEventPackets(src, dst *Chain, rlyPackets []relayPacket, sh *SyncHeaders) {
	// fetch the proofs for the relayPackets
	for _, rp := range rlyPackets {
		if err := rp.FetchCommitResponse(src, dst, sh); err != nil {
			// we don't expect many errors here because of the retry
			// in FetchCommitResponse
			src.Error(err)
		}
	}

	// send the transaction, retrying if not successful
	if err := retry.Do(func() error {
		// instantiate the RelayMsgs with the appropriate update client
		txs := &RelayMsgs{
			Src: []sdk.Msg{
				src.PathEnd.UpdateClient(sh.GetHeader(dst.ChainID), src.MustGetAddress()),
			},
			Dst:          []sdk.Msg{},
			MaxTxSize:    nrs.MaxTxSize,
			MaxMsgLength: nrs.MaxMsgLength,
		}

		// add the packet msgs to RelayPackets
		for _, rp := range rlyPackets {
			txs.Src = append(txs.Src, rp.Msg(src, dst))
		}

		if txs.Send(src, dst); !txs.success {
			return fmt.Errorf("failed to send packets")
		}

		return nil
	}); err != nil {
		src.Error(err)
	}
}

// RelayPacketsUnorderedChan creates transactions to relay un-relayed messages
func (nrs *NaiveStrategy) RelayPacketsUnorderedChan(src, dst *Chain, sp *RelaySequences, sh *SyncHeaders) error {
	// TODO: Implement unordered channels
	return nrs.RelayPacketsOrderedChan(src, dst, sp, sh)
}

// RelayPacketsOrderedChan creates transactions to clear both queues
// CONTRACT: the SyncHeaders passed in here must be up to date or being kept updated
func (nrs *NaiveStrategy) RelayPacketsOrderedChan(src, dst *Chain, sp *RelaySequences, sh *SyncHeaders) error {
	messages := &RelayMsgs{
		Src:          []sdk.Msg{},
		Dst:          []sdk.Msg{},
		MaxTxSize:    nrs.MaxTxSize,
		MaxMsgLength: nrs.MaxMsgLength,
	}

	srcMessages, dstMessages, err := queryPacketMessages(src, dst, sp.Src, sh)
	if err != nil {
		return err
	}

	messages.Src = append(messages.Src, srcMessages...)
	messages.Dst = append(messages.Dst, dstMessages...)

	dstMessages, srcMessages, err = queryPacketMessages(dst, src, sp.Dst, sh)
	if err != nil {
		return err
	}

	messages.Src = append(messages.Src, srcMessages...)
	messages.Dst = append(messages.Dst, dstMessages...)

	if !messages.Ready() {
		return nil
	}

	if len(messages.Src) != 0 {
		messages.Src = append([]sdk.Msg{
			src.PathEnd.UpdateClient(sh.GetHeader(dst.ChainID), src.MustGetAddress()),
		}, messages.Src...)
	}
	if len(messages.Dst) != 0 {
		messages.Dst = append([]sdk.Msg{
			dst.PathEnd.UpdateClient(sh.GetHeader(src.ChainID), dst.MustGetAddress()),
		}, messages.Dst...)
	}

	if messages.Send(src, dst); messages.success {
		if len(messages.Src) > 1 {
			src.logPacketsRelayed(dst, len(messages.Src)-1)
		}
		if len(messages.Dst) > 1 {
			dst.logPacketsRelayed(src, len(messages.Dst)-1)
		}
	}

	return nil
}

func queryPacketMessages(src, dst *Chain, pending []uint64, headers *SyncHeaders) (srcMessages, dstMessages []sdk.Msg, err error) {
	seen := make(map[uint64]bool)
	for _, sequence := range pending {
		seen[sequence] = false
	}

	for _, sequence := range pending {
		if seen[sequence] == true {
			continue
		}

		sequences, types, messages, err := packetMessagesFromTxQuery(src, dst, headers, sequence)
		if err != nil {
			return nil, nil, err
		}

		for index := 0; index < len(sequences); index++ {
			if _, ok := seen[sequences[index]]; !ok {
				continue
			}

			switch types[index] {
			case "recv_packet":
				dstMessages = append(dstMessages, messages[index])
			case "packet_act":
			case "timeout":
				srcMessages = append(srcMessages, messages[index])
			default:
				return nil, nil, fmt.Errorf("invalid packet type")
			}

			seen[sequences[index]] = true
		}
	}

	return srcMessages, dstMessages, nil
}

// packetMessagesFromTxQuery returns a sdk.Msg to relay a packet with a given seq on src
func packetMessagesFromTxQuery(src, dst *Chain, headers *SyncHeaders, sequence uint64) (sequences []uint64, types []string, messages []sdk.Msg, err error) {
	events, err := ParseEvents(fmt.Sprintf(defaultPacketSendQuery, src.PathEnd.ChannelID, sequence))
	if err != nil {
		return nil, nil, nil, err
	}

	res, err := src.QueryTxs(headers.GetHeight(src.ChainID), 1, 1, events)
	if err != nil {
		return nil, nil, nil, err
	}

	for _, tx := range res.Txs {
		packets := relayPacketsFromTxQueryResponse(src.PathEnd, dst.PathEnd, tx.Logs, headers.GetHeight(src.ChainID))
		for _, packet := range packets {
			switch packet.Type() {
			case "recv_packet":
				if err := packet.FetchCommitResponse(dst, src, headers); err != nil {
					return nil, nil, nil, err
				}

				messages = append(messages, packet.Msg(dst, src))
			case "packet_ack":
			case "timeout":
				if err := packet.FetchCommitResponse(src, dst, headers); err != nil {
					return nil, nil, nil, err
				}

				messages = append(messages, packet.Msg(src, dst))
			default:
				return nil, nil, nil, fmt.Errorf("invalid packet type")
			}

			sequences, types = append(sequences, packet.Seq()), append(types, packet.Type())
		}
	}

	return sequences, types, messages, nil
}

// relayPacketsFromTxQueryResponse looks through the events in a sdk.Response
// and returns relayPackets with the appropriate data
func relayPacketsFromTxQueryResponse(src, dst *PathEnd, logs sdk.ABCIMessageLogs, srcHeight uint64) (packets []relayPacket) {
	for _, log := range logs {
		for _, event := range log.Events {
			if event.Type == "send_packet" {
				packet := &relayMsgRecvPacket{}

				for _, attribute := range event.Attributes {
					if attribute.Key == "packet_src_channel" {
						if attribute.Value != src.ChannelID {
							packet.pass = true
							continue
						}
					}
					if attribute.Key == "packet_dst_channel" {
						if attribute.Value != dst.ChannelID {
							packet.pass = true
							continue
						}
					}
					if attribute.Key == "packet_src_port" {
						if attribute.Value != src.PortID {
							packet.pass = true
							continue
						}
					}
					if attribute.Key == "packet_dst_port" {
						if attribute.Value != dst.PortID {
							packet.pass = true
							continue
						}
					}
					if attribute.Key == "packet_data" {
						packet.packetData = []byte(attribute.Value)
					}
					if attribute.Key == "packet_timeout_height" {
						timeout, _ := strconv.ParseUint(attribute.Value, 10, 64)
						packet.timeout = timeout
					}
					if attribute.Key == "packet_timeout_timestamp" {
						timeout, _ := strconv.ParseUint(attribute.Value, 10, 64)
						packet.timeoutStamp = timeout
					}
					if attribute.Key == "packet_sequence" {
						seq, _ := strconv.ParseUint(attribute.Value, 10, 64)
						packet.seq = seq
					}
				}

				switch {
				case srcHeight >= packet.timeout:
					packets = append(packets, packet.timeoutPacket())
				case packet.timeoutStamp != 0 && time.Now().UnixNano() >= int64(packet.timeoutStamp):
					packets = append(packets, packet.timeoutPacket())
				case packet.pass == false:
					packets = append(packets, packet)
				}
			}
		}
	}

	return packets
}

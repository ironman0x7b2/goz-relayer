package cmd

import (
	"fmt"
	"github.com/iqlusioninc/relayer/relayer"
	"github.com/spf13/cobra"
)

// GetStrategyWithOptions sets strategy specific fields.
func GetStrategyWithOptions(cmd *cobra.Command, strategy relayer.Strategy) (relayer.Strategy, error) {
	switch strategy.GetType() {
	case (&relayer.NaiveStrategy{}).GetType():
		ns, ok := strategy.(*relayer.NaiveStrategy)
		if !ok {
			return strategy, fmt.Errorf("strategy.GetType() returns naive, but strategy type (%T) is not type NaiveStrategy", strategy)

		}

		maxTxSize, err := cmd.Flags().GetUint64(flagMaxTxSize)
		if err != nil {
			return ns, err
		}

		ns.MaxTxSize = maxTxSize

		maxMsgLength, err := cmd.Flags().GetUint64(flagMaxMsgLength)
		if err != nil {
			return ns, err
		}

		ns.MaxMsgLength = maxMsgLength

		return ns, nil
	default:
		return strategy, nil
	}
}

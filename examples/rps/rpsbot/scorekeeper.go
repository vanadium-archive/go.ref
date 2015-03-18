package main

import (
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/vlog"
	"v.io/x/ref/examples/rps"
	"v.io/x/ref/examples/rps/common"
	"v.io/x/ref/lib/stats"
	"v.io/x/ref/lib/stats/counter"
)

type ScoreKeeper struct {
	numRecords *counter.Counter
}

func NewScoreKeeper() *ScoreKeeper {
	return &ScoreKeeper{
		numRecords: stats.NewCounter("scorekeeper/num-records"),
	}
}

func (k *ScoreKeeper) Stats() int64 {
	return k.numRecords.Value()
}

func (k *ScoreKeeper) Record(call rpc.ServerCall, score rps.ScoreCard) error {
	b, _ := security.BlessingNames(call.Context(), security.CallSideRemote)
	vlog.VI(1).Infof("Received ScoreCard from %v:", b)
	vlog.VI(1).Info(common.FormatScoreCard(score))
	k.numRecords.Incr(1)
	return nil
}

package main

import (
	"v.io/apps/rps"
	"v.io/apps/rps/common"
	"v.io/core/veyron/lib/stats"
	"v.io/core/veyron/lib/stats/counter"
	"v.io/v23/ipc"
	"v.io/v23/vlog"
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

func (k *ScoreKeeper) Record(ctx ipc.ServerContext, score rps.ScoreCard) error {
	b, _ := ctx.RemoteBlessings().ForContext(ctx)
	vlog.VI(1).Infof("Received ScoreCard from %v:", b)
	vlog.VI(1).Info(common.FormatScoreCard(score))
	k.numRecords.Incr(1)
	return nil
}

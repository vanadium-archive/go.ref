package main

import (
	"v.io/apps/rps"
	"v.io/apps/rps/common"
	"v.io/veyron/veyron/lib/stats"
	"v.io/veyron/veyron/lib/stats/counter"
	"v.io/veyron/veyron2/ipc"
	"v.io/veyron/veyron2/vlog"
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
	vlog.VI(1).Infof("Received ScoreCard from %v:", ctx.RemoteBlessings().ForContext(ctx))
	vlog.VI(1).Info(common.FormatScoreCard(score))
	k.numRecords.Incr(1)
	return nil
}

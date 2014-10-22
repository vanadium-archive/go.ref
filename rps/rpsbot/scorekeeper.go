package main

import (
	"veyron.io/apps/rps"
	"veyron.io/apps/rps/common"
	"veyron.io/veyron/veyron/lib/stats"
	"veyron.io/veyron/veyron/lib/stats/counter"
	"veyron.io/veyron/veyron2/ipc"
	"veyron.io/veyron/veyron2/vlog"
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

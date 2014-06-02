package impl

import (
	rps "veyron/examples/rockpaperscissors"
	"veyron/examples/rockpaperscissors/common"

	"veyron2/ipc"
	"veyron2/vlog"
)

type ScoreKeeper struct {
	numRecords common.Counter
}

func NewScoreKeeper() *ScoreKeeper {
	return &ScoreKeeper{}
}

func (k *ScoreKeeper) Stats() int64 {
	return k.numRecords.Value()
}

func (k *ScoreKeeper) Record(ctx ipc.ServerContext, score rps.ScoreCard) error {
	vlog.Infof("Received ScoreCard from %s:", ctx.RemoteID())
	vlog.Info(common.FormatScoreCard(score))
	k.numRecords.Add(1)
	return nil
}

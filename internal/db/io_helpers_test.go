package db

import "github.com/Wirezat/production-optimizer/internal/resource"

func itemIO(mod, id string, n int64) resource.IO {
	return resource.IO{Ref: resource.Ref{ModID: mod, ID: id}, Amount: resource.NewRational(n, 1), Prob: resource.NewRational(1, 1), Consumed: true}
}

func fluidIO(mod, id string, mb int64) resource.IO {
	return resource.IO{Ref: resource.Ref{ModID: mod, ID: id, Kind: resource.KindFluid}, Amount: resource.NewRational(mb, 1), Prob: resource.NewRational(1, 1), Consumed: true}
}

func tagIO(kind resource.Kind, tag string, n int64) resource.IO {
	return resource.IO{Ref: resource.Ref{TagRef: tag, Kind: kind}, Amount: resource.NewRational(n, 1), Prob: resource.NewRational(1, 1), Consumed: true}
}

package main

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/caps"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/clob"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/halt"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/handler"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/signer"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/store"
)

func main() {
	svc.Run("trader", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded (secrets redacted)",
		"exchange", cfg.ExchangeAddress, "mode", cfg.Policy.Mode,
		"perTrade", cfg.PerTradeUSD, "perDay", cfg.PerDayUSD, "exposure", cfg.ExposureUSD,
		"nats", cfg.NATSURL, "clob", cfg.CLOBBase)

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	sgn, err := signer.New(cfg.PrivateKey, cfg.MakerAddress, cfg.ExchangeAddress, cfg.SignatureType)
	if err != nil {
		return err
	}

	pub, err := bus.Connect(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer pub.Close()
	sub, err := bus.NewSubscriber(cfg.NATSURL, log)
	if err != nil {
		return err
	}
	defer sub.Close()

	hs := halt.New()
	c := caps.New(cfg.PerTradeUSD, cfg.PerDayUSD, cfg.ExposureUSD, st, log)
	clobClient := clob.New(cfg.CLOBBase, cfg.Creds, nil)
	h := handler.New(c, sgn, clobClient, st, pub, hs, cfg.Policy, log)

	// Kill switch: every control.halt updates the local halt state.
	if _, err := sub.OnControl(func(m bus.ControlMsg) {
		hs.Set(m.Halted)
		log.Warn("control.halt received", "halted", m.Halted, "by", m.By)
	}); err != nil {
		return err
	}
	// Approved orders execute.
	if _, err := sub.OnOrderApproved(cfg.QueueGroup, func(d bus.OrderDecision) {
		mctx, cancel := svc.Detached(ctx)
		defer cancel()
		h.OnApproved(mctx, d)
	}); err != nil {
		return err
	}
	// Proposals auto-execute only when the policy doesn't require approval.
	if _, err := sub.OnOrderProposed(cfg.QueueGroup, func(p bus.OrderProposal) {
		mctx, cancel := svc.Detached(ctx)
		defer cancel()
		h.OnProposed(mctx, p)
	}); err != nil {
		return err
	}

	log.Info("trader executing (sole key holder; caps + halt enforced)")
	<-ctx.Done()
	return nil
}

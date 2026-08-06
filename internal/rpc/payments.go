package rpc

import (
	"context"

	"github.com/iamxvbaba/td/tg"
	"github.com/iamxvbaba/td/tgerr"

	"github.com/iamxvbaba/td/tlprofile"
	"telesrv/internal/compat/tdesktop"
)

// registerPayments 注册 payments.* RPC。telesrv 不实现 Stars/Star Gift 经济：
// 大部分曾经的 Stars/gift 动作 RPC 已不再注册；仍注册的几个只读状态 RPC
// （getStarsStatus/Subscriptions/Transactions/RevenueStats）返回固定空值，
// 因为部分客户端界面会无条件加载它们——错误返回会导致界面卡死或崩溃，
// 空值则让界面正常渲染成"没有 Stars"。
func (r *Router) registerPayments(d *tlprofile.Dispatcher) {
	registerRPC[*tg.PaymentsCanPurchaseStoreRequest](d, tlprofile.SemanticMethodPaymentsCanPurchaseStore, func(ctx context.Context, req *tg.PaymentsCanPurchaseStoreRequest) (any, error) {
		return r.onPaymentsCanPurchaseStore(ctx, req)
	})
	registerRPC[*tg.PaymentsAssignPlayMarketTransactionRequest](d, tlprofile.SemanticMethodPaymentsAssignPlayMarketTransaction, func(ctx context.Context, req *tg.PaymentsAssignPlayMarketTransactionRequest) (any, error) {
		return r.onPaymentsAssignPlayMarketTransaction(ctx, req)
	})
	registerRPC[*tg.PaymentsGetPremiumGiftCodeOptionsRequest](d, tlprofile.SemanticMethodPaymentsGetPremiumGiftCodeOptions, func(ctx context.Context, req *tg.PaymentsGetPremiumGiftCodeOptionsRequest) (any, error) {
		return []tg.PremiumGiftCodeOption{}, nil
	})
	registerRPC[*tg.PaymentsGetStarsStatusRequest](d, tlprofile.SemanticMethodPaymentsGetStarsStatus, func(ctx context.Context, layerRequest *tg.PaymentsGetStarsStatusRequest) (any, error) {
		return r.onPaymentsGetStarsStatus(ctx, layerRequest)
	})
	registerRPC[*tg.PaymentsGetStarsSubscriptionsRequest](d, tlprofile.SemanticMethodPaymentsGetStarsSubscriptions, func(ctx context.Context, req *tg.PaymentsGetStarsSubscriptionsRequest) (any, error) {
		return r.onPaymentsGetStarsSubscriptions(ctx, req)
	})
	registerRPC[*tg.PaymentsGetStarsTransactionsRequest](d, tlprofile.SemanticMethodPaymentsGetStarsTransactions, func(ctx context.Context, layerRequest *tg.PaymentsGetStarsTransactionsRequest) (any, error) {
		return r.onPaymentsGetStarsTransactions(ctx, layerRequest)
	})
	registerRPC[*tg.PaymentsGetStarsRevenueAdsAccountURLRequest](d, tlprofile.SemanticMethodPaymentsGetStarsRevenueAdsAccountURL, func(ctx context.Context, layerRequest *tg.PaymentsGetStarsRevenueAdsAccountURLRequest) (any, error) {
		peer := layerRequest.
			Peer
		_ = peer

		userID, _, err := r.currentUserID(ctx)
		if err != nil {
			return nil, internalErr()
		}
		if _, err := r.checkedDomainPeerFromInputPeer(ctx, userID, peer); err != nil {
			return nil, err
		}
		return &tg.PaymentsStarsRevenueAdsAccountURL{URL: r.publicLink("")}, nil
	})
	registerRPC[*tg.PaymentsGetStarsRevenueStatsRequest](d, tlprofile.SemanticMethodPaymentsGetStarsRevenueStats, func(ctx context.Context, req *tg.PaymentsGetStarsRevenueStatsRequest) (any, error) {
		return r.onPaymentsGetStarsRevenueStats(ctx, req)
	})

}

func (r *Router) onPaymentsCanPurchaseStore(ctx context.Context, _ *tg.PaymentsCanPurchaseStoreRequest) (bool, error) {
	if _, _, err := r.currentUserID(ctx); err != nil {
		return false, internalErr()
	}
	// telesrv deliberately exposes no Google Play products or receipt verifier.
	// DrKLO is steered to the invoice flow by appConfig; if a stale client still
	// reaches this preflight, fail closed instead of authorizing an unverifiable
	// external charge.
	return false, nil
}

func (r *Router) onPaymentsAssignPlayMarketTransaction(ctx context.Context, _ *tg.PaymentsAssignPlayMarketTransactionRequest) (tg.UpdatesClass, error) {
	if _, _, err := r.currentUserID(ctx); err != nil {
		return nil, internalErr()
	}
	return nil, tgerr.New(400, "STORE_PAYMENT_UNAVAILABLE")
}

// onPaymentsGetStarsRevenueStats: telesrv has no Stars/gift economy, so every
// peer gets the same zero-balance compatibility response (no channel ledger
// branch left to read from).
func (r *Router) onPaymentsGetStarsRevenueStats(ctx context.Context, req *tg.PaymentsGetStarsRevenueStatsRequest) (*tg.PaymentsStarsRevenueStats, error) {
	userID, _, err := r.currentUserID(ctx)
	if err != nil {
		return nil, internalErr()
	}
	if req == nil {
		return nil, peerIDInvalidErr()
	}
	if _, err := r.checkedDomainPeerFromInputPeer(ctx, userID, req.Peer); err != nil {
		return nil, err
	}
	return tdesktop.StarsRevenueStats(req.GetTon()), nil
}

// onPaymentsGetStarsStatus 无 Stars 账本，恒返回零余额（响应仍是合法的
// payments.starsStatus——两端客户端无条件读取 balance 字段）。
func (r *Router) onPaymentsGetStarsStatus(ctx context.Context, req *tg.PaymentsGetStarsStatusRequest) (*tg.PaymentsStarsStatus, error) {
	if _, _, err := r.currentUserID(ctx); err != nil {
		return nil, internalErr()
	}
	if req != nil && req.GetTon() {
		return emptyStarsStatus(&tg.StarsTonAmount{}), nil
	}
	return emptyStarsStatus(&tg.StarsAmount{}), nil
}

// onPaymentsGetStarsSubscriptions returns a zero balance and no subscriptions
// — telesrv never creates recurring Stars subscriptions.
func (r *Router) onPaymentsGetStarsSubscriptions(ctx context.Context, req *tg.PaymentsGetStarsSubscriptionsRequest) (*tg.PaymentsStarsStatus, error) {
	if _, _, err := r.currentUserID(ctx); err != nil {
		return nil, internalErr()
	}
	return emptyStarsStatus(&tg.StarsAmount{}), nil
}

// onPaymentsGetStarsTransactions 无 Stars 账本，恒返回零余额、空流水（不设置
// next_offset，避免客户端无限翻页）。
func (r *Router) onPaymentsGetStarsTransactions(ctx context.Context, req *tg.PaymentsGetStarsTransactionsRequest) (*tg.PaymentsStarsStatus, error) {
	if _, _, err := r.currentUserID(ctx); err != nil {
		return nil, internalErr()
	}
	if req != nil && req.GetTon() {
		return emptyStarsStatus(&tg.StarsTonAmount{}), nil
	}
	return emptyStarsStatus(&tg.StarsAmount{}), nil
}

// emptyStarsStatus 构造一个合法的最小 payments.starsStatus（chats/users 非空 vector 但可空）。
func emptyStarsStatus(balance tg.StarsAmountClass) *tg.PaymentsStarsStatus {
	return &tg.PaymentsStarsStatus{
		Balance: balance,
		Chats:   []tg.ChatClass{},
		Users:   []tg.UserClass{},
	}
}

package postgres

import (
	"context"
	"fmt"

	"telesrv/internal/domain"
)

// SetActiveCall 写入/清除 channel 行上的活跃群通话关联。
func (s *ChannelStore) SetActiveCall(ctx context.Context, channelID, callID, callAccessHash int64, notEmpty bool) (domain.Channel, error) {
	if channelID == 0 {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	if callID == 0 {
		callAccessHash = 0
		notEmpty = false
	}
	tag, err := s.db.Exec(ctx, `
UPDATE channels
SET active_call_id = $2, active_call_access_hash = $3, active_call_not_empty = $4, updated_at = now()
WHERE id = $1 AND NOT deleted`, channelID, callID, callAccessHash, notEmpty)
	if err != nil {
		return domain.Channel{}, fmt.Errorf("set channel active call: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Channel{}, domain.ErrChannelInvalid
	}
	return getChannelByID(ctx, s.db, channelID)
}

// AppendCallServiceMessage 生成群通话服务消息（started/ended/invite，带频道 pts）。
func (s *ChannelStore) AppendCallServiceMessage(ctx context.Context, channelID, senderUserID int64, date int, action domain.ChannelMessageAction) (domain.SendChannelMessageResult, error) {
	return s.appendServiceMessage(ctx, "call", channelID, senderUserID, date, action)
}

func (s *ChannelStore) appendServiceMessage(ctx context.Context, label string, channelID, senderUserID int64, date int, action domain.ChannelMessageAction) (domain.SendChannelMessageResult, error) {
	if channelID == 0 || senderUserID == 0 {
		return domain.SendChannelMessageResult{}, domain.ErrChannelInvalid
	}
	beginner, ok := s.db.(txBeginner)
	if !ok {
		return domain.SendChannelMessageResult{}, fmt.Errorf("append %s service message: db does not support transactions", label)
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return domain.SendChannelMessageResult{}, fmt.Errorf("begin %s service message: %w", label, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	channel, err := getChannelByID(ctx, tx, channelID)
	if err != nil {
		return domain.SendChannelMessageResult{}, err
	}
	msg, event, err := s.insertServiceMessage(ctx, tx, channel, senderUserID, date, action)
	if err != nil {
		return domain.SendChannelMessageResult{}, err
	}
	channel.TopMessageID = msg.ID
	channel.Pts = event.Pts
	if err := tx.Commit(ctx); err != nil {
		return domain.SendChannelMessageResult{}, fmt.Errorf("commit %s service message: %w", label, err)
	}
	committed = true
	recipients, _ := s.ListActiveChannelMemberIDs(ctx, 0, channelID, 0)
	return domain.SendChannelMessageResult{
		Channel:    channel,
		Message:    msg,
		Event:      event,
		Recipients: recipients,
	}, nil
}

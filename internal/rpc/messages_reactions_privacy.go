package rpc

import (
	"context"
	"strings"

	"github.com/iamxvbaba/td/tg"

	"telesrv/internal/domain"
)

func (r *Router) onMessagesSetDefaultReaction(ctx context.Context, reaction tg.ReactionClass) (bool, error) {
	userID, _, err := r.currentUserID(ctx)
	if err != nil {
		return false, internalErr()
	}
	parsed, err := domainMessageReactionFromTL(reaction)
	if err != nil {
		return false, err
	}
	if err := r.validateDefaultReaction(ctx, parsed); err != nil {
		return false, err
	}
	if svc, ok := r.deps.Account.(accountDefaultReactionService); ok {
		if _, err := svc.SetDefaultReaction(ctx, userID, parsed); err != nil {
			return false, internalErr()
		}
	}
	return true, nil
}

func (r *Router) validateDefaultReaction(ctx context.Context, reaction domain.MessageReaction) error {
	if reaction.Type == domain.MessageReactionCustomEmoji {
		return nil
	}
	if reaction.Type != domain.MessageReactionEmoji {
		return reactionInvalidErr()
	}

	if r.deps.Files != nil {
		catalog, err := r.deps.Files.ListAvailableReactions(ctx)
		if err != nil {
			return internalErr()
		}
		if len(catalog) > 0 {
			for _, item := range catalog {
				if !item.Inactive && strings.TrimSpace(item.Reaction) == reaction.Emoticon {
					return nil
				}
			}
			return reactionInvalidErr()
		}
	}

	for _, item := range staticReactionCatalog() {
		if item.Key() == reaction.Key() {
			return nil
		}
	}
	return reactionInvalidErr()
}

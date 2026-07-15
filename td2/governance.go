package tenderduty

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type govProposal struct {
	ID      string
	Title   string
	EndTime string
}

var (
	alertedProposals    = map[string]map[string]bool{}
	alertedProposalsMux sync.Mutex
)

func (c *Config) startGovernanceMonitor() {
	type chainEntry struct {
		name   string
		lcdUrl string
	}

	c.chainsMux.RLock()
	var chains []chainEntry
	for name, cc := range c.Chains {
		if cc.LcdUrl != "" {
			chains = append(chains, chainEntry{name: name, lcdUrl: cc.LcdUrl})
		}
	}
	c.chainsMux.RUnlock()

	if len(chains) == 0 {
		return
	}

	l("governance monitor started for", len(chains), "chains")

	for {
		for _, entry := range chains {
			proposals, err := fetchVotingProposals(entry.lcdUrl)
			if err != nil {
				l(fmt.Sprintf("governance: %s: %s", entry.name, err))
				continue
			}

			alertedProposalsMux.Lock()
			if alertedProposals[entry.name] == nil {
				alertedProposals[entry.name] = map[string]bool{}
			}

			for _, p := range proposals {
				if alertedProposals[entry.name][p.ID] {
					continue
				}
				alertedProposals[entry.name][p.ID] = true

				c.chainsMux.RLock()
				cc := c.Chains[entry.name]
				tgKey := firstNonEmpty(cc.Alerts.Telegram.ApiKey, c.Telegram.ApiKey)
				tgChan := firstNonEmpty(cc.Alerts.Telegram.Channel, c.Telegram.Channel)
				tgEnabled := c.Telegram.Enabled && cc.Alerts.Telegram.Enabled
				c.chainsMux.RUnlock()

				if !tgEnabled || tgKey == "" || tgChan == "" {
					continue
				}

				text := fmt.Sprintf(
					"[RuangNode] 🗳️ New Proposal: *%s*\n\n📋 #%s: %s\n⏰ Voting ends: %s",
					entry.name, p.ID, p.Title, p.EndTime,
				)
				l(fmt.Sprintf("governance: new proposal #%s on %s", p.ID, entry.name))
				sendTgMessage(tgKey, tgChan, text)
			}
			alertedProposalsMux.Unlock()
		}
		time.Sleep(5 * time.Minute)
	}
}

func sendTgMessage(apiKey, channel, text string) {
	bot, err := tgbotapi.NewBotAPI(apiKey)
	if err != nil {
		l("governance tg:", err)
		return
	}
	var msg tgbotapi.MessageConfig
	if chatID, err := strconv.ParseInt(channel, 10, 64); err == nil {
		msg = tgbotapi.NewMessage(chatID, text)
	} else {
		msg = tgbotapi.NewMessageToChannel(channel, text)
	}
	msg.ParseMode = "Markdown"
	if _, err := bot.Send(msg); err != nil {
		l("governance tg send:", err)
	}
}

func fetchVotingProposals(lcdUrl string) ([]govProposal, error) {
	base := lcdUrl
	base = strings.TrimPrefix(base, "tcp://")
	if !strings.HasPrefix(base, "http") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")

	for _, path := range []string{
		"/cosmos/gov/v1/proposals?proposal_status=PROPOSAL_STATUS_VOTING_PERIOD&pagination.limit=50",
		"/cosmos/gov/v1beta1/proposals?proposal_status=2&pagination.limit=50",
		"/atomone/gov/v1/proposals?proposal_status=PROPOSAL_STATUS_VOTING_PERIOD&pagination.limit=50",
		"/atomone/gov/v1beta1/proposals?proposal_status=2&pagination.limit=50",
	} {
		proposals, err := queryProposals(base + path)
		if err == nil {
			return proposals, nil
		}
	}
	return nil, fmt.Errorf("could not fetch proposals from %s", lcdUrl)
}

func queryProposals(url string) ([]govProposal, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	//#nosec G107
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// try gov v1
	var v1 struct {
		Proposals []struct {
			ID            string `json:"id"`
			Title         string `json:"title"`
			VotingEndTime string `json:"voting_end_time"`
		} `json:"proposals"`
	}
	if json.Unmarshal(body, &v1) == nil && len(v1.Proposals) > 0 {
		var out []govProposal
		for _, p := range v1.Proposals {
			out = append(out, govProposal{ID: p.ID, Title: p.Title, EndTime: fmtTime(p.VotingEndTime)})
		}
		return out, nil
	}

	// try gov v1beta1
	var v1b struct {
		Proposals []struct {
			ProposalID    string `json:"proposal_id"`
			Content       struct{ Title string `json:"title"` } `json:"content"`
			VotingEndTime string `json:"voting_end_time"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal(body, &v1b); err != nil {
		return nil, err
	}
	var out []govProposal
	for _, p := range v1b.Proposals {
		out = append(out, govProposal{ID: p.ProposalID, Title: p.Content.Title, EndTime: fmtTime(p.VotingEndTime)})
	}
	return out, nil
}

func fmtTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format("02 Jan 2006 15:04 UTC")
}

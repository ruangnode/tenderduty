package tenderduty

import (
	"fmt"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (c *Config) startTgCommandListener() {
	if !c.Telegram.Enabled || c.Telegram.ApiKey == "" {
		return
	}

	bot, err := tgbotapi.NewBotAPI(c.Telegram.ApiKey)
	if err != nil {
		l("tg command listener:", err)
		return
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil || !update.Message.IsCommand() {
			continue
		}
		switch update.Message.Command() {
		case "status":
			arg := strings.TrimSpace(update.Message.CommandArguments())
			reply := c.handleStatusCommand(arg)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, reply)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
		case "list":
			reply := c.handleListCommand()
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, reply)
			msg.ParseMode = "Markdown"
			bot.Send(msg)
		}
	}
}

func (c *Config) handleListCommand() string {
	c.chainsMux.RLock()
	defer c.chainsMux.RUnlock()

	if len(c.Chains) == 0 {
		return "No chains configured."
	}

	lines := []string{"📋 *Chains monitored:*\n"}
	for name, cc := range c.Chains {
		nodeStatus := fmt.Sprintf("%d/%d nodes", len(cc.Nodes)-countDownNodes(cc), len(cc.Nodes))
		valStatus := "⏳"
		if cc.valInfo != nil && cc.valInfo.Moniker != "not connected" {
			if cc.valInfo.Tombstoned {
				valStatus = "☠️"
			} else if cc.valInfo.Jailed {
				valStatus = "🔴"
			} else if cc.valInfo.Bonded {
				valStatus = "🟢"
			} else {
				valStatus = "⚪"
			}
		}
		lines = append(lines, fmt.Sprintf("%s *%s* — %s", valStatus, name, nodeStatus))
	}
	return strings.Join(lines, "\n")
}

func countDownNodes(cc *ChainConfig) int {
	n := 0
	for _, node := range cc.Nodes {
		if node.down {
			n++
		}
	}
	return n
}

func (c *Config) handleStatusCommand(chainName string) string {
	c.chainsMux.RLock()
	defer c.chainsMux.RUnlock()

	if chainName == "" {
		var names []string
		for name := range c.Chains {
			names = append(names, name)
		}
		return "Usage: /status <chain>\nAvailable: " + strings.Join(names, ", ")
	}

	var cc *ChainConfig
	var foundName string
	for name, chain := range c.Chains {
		if strings.EqualFold(name, chainName) {
			cc = chain
			foundName = name
			break
		}
	}

	if cc == nil {
		var names []string
		for name := range c.Chains {
			names = append(names, name)
		}
		return fmt.Sprintf("Chain '%s' not found.\nAvailable: %s", chainName, strings.Join(names, ", "))
	}

	if cc.valInfo == nil || cc.valInfo.Moniker == "not connected" {
		return fmt.Sprintf("⏳ *%s*: not connected yet", foundName)
	}

	status := "🟢 Bonded"
	if cc.valInfo.Tombstoned {
		status = "☠️ Tombstoned"
	} else if cc.valInfo.Jailed {
		status = "🔴 Jailed"
	} else if !cc.valInfo.Bonded {
		status = "⚪ Not Active"
	}

	uptime := "error"
	if cc.valInfo.Window > 0 {
		if cc.valInfo.Missed == 0 {
			uptime = "100%"
		} else {
			uptime = fmt.Sprintf("%.2f%%", 100-float64(cc.valInfo.Missed)/float64(cc.valInfo.Window)*100)
		}
	}

	healthyNodes := 0
	for _, node := range cc.Nodes {
		if !node.down {
			healthyNodes++
		}
	}

	lastBlock := "unknown"
	if !cc.lastBlockTime.IsZero() {
		ago := time.Since(cc.lastBlockTime).Round(time.Second)
		lastBlock = fmt.Sprintf("%d (%s ago)", cc.lastBlockNum, ago)
	}

	return fmt.Sprintf(
		"📊 *%s* (`%s`)\n\n"+
			"Moniker: `%s`\n"+
			"Status: %s\n"+
			"Uptime: %s (%d/%d missed)\n"+
			"Consecutive missed: %d\n"+
			"Last block: %s\n"+
			"Nodes: %d/%d healthy",
		foundName, cc.ChainId,
		cc.valInfo.Moniker,
		status,
		uptime, cc.valInfo.Missed, cc.valInfo.Window,
		int(cc.statConsecutiveMiss),
		lastBlock,
		healthyNodes, len(cc.Nodes),
	)
}

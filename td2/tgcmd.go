package tenderduty

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type addChainSession struct {
	step    int
	name    string
	chainID string
	valoper string
	rpcURL  string
}

var (
	addChainSessions   = map[int64]*addChainSession{}
	addChainSessionMux sync.Mutex
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

	send := func(chatID int64, text string) {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		bot.Send(msg)
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}
		chatID := update.Message.Chat.ID

		// handle ongoing addchain session (non-command replies)
		addChainSessionMux.Lock()
		session, inSession := addChainSessions[chatID]
		addChainSessionMux.Unlock()

		if inSession && !update.Message.IsCommand() {
			c.handleAddChainStep(chatID, update.Message.Text, session, send)
			continue
		}

		if !update.Message.IsCommand() {
			continue
		}

		switch update.Message.Command() {
		case "status":
			arg := strings.TrimSpace(update.Message.CommandArguments())
			send(chatID, c.handleStatusCommand(arg))
		case "list":
			send(chatID, c.handleListCommand())
		case "addchain":
			addChainSessionMux.Lock()
			addChainSessions[chatID] = &addChainSession{step: 0}
			addChainSessionMux.Unlock()
			send(chatID, "➕ *Add Chain*\n\nStep 1/4: Masukkan nama chain:\n(contoh: `AtomOne Testnet`)")
		case "cancel":
			addChainSessionMux.Lock()
			delete(addChainSessions, chatID)
			addChainSessionMux.Unlock()
			send(chatID, "❌ Dibatalkan.")
		}
	}
}

func (c *Config) handleAddChainStep(chatID int64, text string, s *addChainSession, send func(int64, string)) {
	switch s.step {
	case 0:
		s.name = strings.TrimSpace(text)
		s.step = 1
		send(chatID, fmt.Sprintf("✅ Nama: `%s`\n\nStep 2/4: Masukkan chain ID:\n(contoh: `atomone-testnet-1`)", s.name))
	case 1:
		s.chainID = strings.TrimSpace(text)
		s.step = 2
		send(chatID, fmt.Sprintf("✅ Chain ID: `%s`\n\nStep 3/4: Masukkan valoper address:", s.chainID))
	case 2:
		s.valoper = strings.TrimSpace(text)
		s.step = 3
		send(chatID, fmt.Sprintf("✅ Valoper: `%s`\n\nStep 4/4: Masukkan RPC URL:\n(contoh: `tcp://10.32.0.2:26657`)", s.valoper))
	case 3:
		s.rpcURL = strings.TrimSpace(text)
		s.step = 4
		send(chatID, fmt.Sprintf(
			"📋 *Konfirmasi:*\n\nNama: `%s`\nChain ID: `%s`\nValoper: `%s`\nRPC: `%s`\n\nKirim `ya` untuk simpan atau `tidak` untuk batal.",
			s.name, s.chainID, s.valoper, s.rpcURL,
		))
	case 4:
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "ya", "yes", "y":
			err := c.appendChainToConfig(s)
			addChainSessionMux.Lock()
			delete(addChainSessions, chatID)
			addChainSessionMux.Unlock()
			if err != nil {
				send(chatID, "❌ Gagal menyimpan: "+err.Error())
				return
			}
			send(chatID, fmt.Sprintf("✅ Chain *%s* berhasil ditambahkan!\n\nContainer akan restart dalam 3 detik...", s.name))
			time.Sleep(3 * time.Second)
			os.Exit(0) // Docker akan restart otomatis
		default:
			addChainSessionMux.Lock()
			delete(addChainSessions, chatID)
			addChainSessionMux.Unlock()
			send(chatID, "❌ Dibatalkan.")
		}
	}
}

func (c *Config) appendChainToConfig(s *addChainSession) error {
	if c.configFile == "" {
		c.configFile = "config.yml"
	}
	f, err := os.OpenFile(c.configFile, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	block := fmt.Sprintf(`
  %q:
    chain_id: %q
    valoper_address: %q
    public_fallback: no

    alerts:
      stalled_enabled: yes
      stalled_minutes: 10
      consecutive_enabled: yes
      consecutive_missed: 5
      consecutive_priority: critical
      percentage_enabled: yes
      percentage_missed: 10
      percentage_priority: warning
      alert_if_inactive: yes
      alert_if_no_servers: yes
      telegram:
        enabled: yes
        api_key: ""
        channel: ""

    nodes:
      - url: %s
        alert_if_down: yes
`, s.name, s.chainID, s.valoper, s.rpcURL)

	_, err = f.WriteString(block)
	return err
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

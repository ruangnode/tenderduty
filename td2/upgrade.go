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
)

type upgradePlan struct {
	Name   string
	Height int64
	Info   string
}

var (
	alertedUpgrades    = map[string]map[string]bool{} // chain -> "name:threshold" -> sent
	alertedUpgradesMux sync.Mutex
)

func lcdBase(lcdUrl string) string {
	s := strings.TrimPrefix(lcdUrl, "tcp://")
	if !strings.HasPrefix(s, "http") {
		s = "http://" + s
	}
	return strings.TrimRight(s, "/")
}

func fetchUpgradePlan(lcdUrl string) (*upgradePlan, error) {
	url := lcdBase(lcdUrl) + "/cosmos/upgrade/v1beta1/current_plan"
	client := &http.Client{Timeout: 10 * time.Second}
	//#nosec G107
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Plan *struct {
			Name   string `json:"name"`
			Height string `json:"height"`
			Info   string `json:"info"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Plan == nil {
		return nil, nil // no upgrade scheduled
	}
	h, err := strconv.ParseInt(result.Plan.Height, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid height %q", result.Plan.Height)
	}
	return &upgradePlan{Name: result.Plan.Name, Height: h, Info: result.Plan.Info}, nil
}

// startUpgradeMonitor checks every 2 minutes and alerts at 500 / 100 / 10 blocks remaining.
func (c *Config) startUpgradeMonitor() {
	thresholds := []int64{500, 100, 10}

	for {
		type entry struct {
			name     string
			lcdUrl   string
			blockNum int64
		}
		c.chainsMux.RLock()
		var entries []entry
		for name, cc := range c.Chains {
			if cc.LcdUrl != "" {
				entries = append(entries, entry{name, cc.LcdUrl, cc.lastBlockNum})
			}
		}
		c.chainsMux.RUnlock()

		for _, e := range entries {
			if e.blockNum == 0 {
				continue // not yet connected
			}

			plan, err := fetchUpgradePlan(e.lcdUrl)
			if err != nil {
				l(fmt.Sprintf("upgrade monitor %s: %s", e.name, err))
				continue
			}
			if plan == nil {
				// no pending upgrade — clear state so future upgrades alert fresh
				alertedUpgradesMux.Lock()
				delete(alertedUpgrades, e.name)
				alertedUpgradesMux.Unlock()
				continue
			}

			remaining := plan.Height - e.blockNum
			if remaining < 0 {
				// upgrade height already passed
				alertedUpgradesMux.Lock()
				delete(alertedUpgrades, e.name)
				alertedUpgradesMux.Unlock()
				continue
			}

			c.chainsMux.RLock()
			cc := c.Chains[e.name]
			tgKey := firstNonEmpty(cc.Alerts.Telegram.ApiKey, c.Telegram.ApiKey)
			tgChan := firstNonEmpty(cc.Alerts.Telegram.Channel, c.Telegram.Channel)
			tgEnabled := c.Telegram.Enabled
			c.chainsMux.RUnlock()

			alertedUpgradesMux.Lock()
			if alertedUpgrades[e.name] == nil {
				alertedUpgrades[e.name] = map[string]bool{}
			}
			for _, thr := range thresholds {
				if remaining > thr {
					continue
				}
				key := fmt.Sprintf("%s:%d", plan.Name, thr)
				if alertedUpgrades[e.name][key] {
					break // already sent this level
				}
				alertedUpgrades[e.name][key] = true

				if tgEnabled && tgKey != "" && tgChan != "" {
					urgency := "📢 NOTICE"
					if thr <= 10 {
						urgency = "🚨 CRITICAL"
					} else if thr <= 100 {
						urgency = "⚠️ URGENT"
					}
					// estimate ~6s per block average
					estTime := time.Now().Add(time.Duration(remaining*6) * time.Second)
					text := fmt.Sprintf(
						"[RuangNode] %s — Chain Upgrade!\n\n"+
							"⛓️ Chain: *%s*\n"+
							"🔖 Version: `%s`\n"+
							"📦 Upgrade Height: `%d`\n"+
							"🔢 Blocks Remaining: `%d`\n"+
							"⏰ Est. Time: %s",
						urgency, e.name, plan.Name, plan.Height, remaining,
						estTime.UTC().Format("02 Jan 2006 15:04 UTC"),
					)
					go sendTgMessage(tgKey, tgChan, text)
					l(fmt.Sprintf("upgrade alert sent: %s upgrade %q in %d blocks", e.name, plan.Name, remaining))
				}
				break // send only the most urgent threshold per cycle
			}
			alertedUpgradesMux.Unlock()
		}

		time.Sleep(2 * time.Minute)
	}
}

// upgradeStatusText returns a human-readable upgrade status for a single chain.
func upgradeStatusText(name string, plan *upgradePlan, currentBlock int64) string {
	if plan == nil {
		return fmt.Sprintf("✅ *%s*: no pending upgrade", name)
	}
	remaining := plan.Height - currentBlock
	if remaining < 0 {
		return fmt.Sprintf("✅ *%s*: upgrade `%s` already applied (was at block %d)", name, plan.Name, plan.Height)
	}
	estTime := time.Now().Add(time.Duration(remaining*6) * time.Second)
	return fmt.Sprintf(
		"🔖 *%s* — Upgrade `%s`\n"+
			"📦 Block: `%d`\n"+
			"🔢 Remaining: `%d` blocks\n"+
			"⏰ Est: %s",
		name, plan.Name, plan.Height, remaining,
		estTime.UTC().Format("02 Jan 2006 15:04 UTC"),
	)
}

// Block status → color (values from ws.go StatusType enum)
// 0=missed  1=prevote  2=precommit  3=signed  4=proposed  -1=nodata
const TL_COLOR = {
    4: '#3b82f6',  // proposer  – blue
    3: '#10b981',  // signed    – emerald
    2: '#f97316',  // precommit – orange
    1: '#a855f7',  // prevote   – violet
    0: '#ef4444',  // missed    – red
}

function legend() {
    // Legend is static HTML — nothing to render
}

function drawSeries(status) {
    const skeleton  = document.getElementById('tl-skeleton')
    const container = document.getElementById('timelineRows')
    if (!container) return

    // No chains yet — show skeleton and return
    if (!status || !status.Status || status.Status.length === 0) {
        if (skeleton) skeleton.style.display = ''
        container.style.display = 'none'
        container.innerHTML = ''
        return
    }

    // Calculate how many bars fit in available width
    const nameW = 165 + 10   // tl-name width + gap
    const barW  = 5, gap = 1
    const avail = Math.max(80, container.offsetWidth - nameW)
    const slots = Math.min(512, Math.floor(avail / (barW + gap)))

    let allEmpty = true
    let html = ''

    for (const s of status.Status) {
        const blocks = s.blocks || []
        const hasSomeData = blocks.some(b => b !== -1 && b !== undefined)
        if (hasSomeData) allEmpty = false

        const show = blocks.length > slots ? blocks.slice(blocks.length - slots) : blocks

        let bars = ''
        if (show.length === 0) {
            bars = '<span style="font-size:10px;color:var(--muted);padding-left:4px">waiting…</span>'
        } else {
            for (const b of show) {
                const c = TL_COLOR[b]
                bars += c
                    ? `<span class="tl-bar" style="--bc:${c}"></span>`
                    : `<span class="tl-bar-nd"></span>`
            }
        }

        html += `<div class="tl-row">` +
            `<span class="tl-name" title="${_.escape(s.name)}">${_.escape(s.name)}</span>` +
            `<div class="tl-bars">${bars}</div>` +
            `</div>`
    }

    // If every chain has only no-data (-1) blocks, show informative state inside rows
    // but still render the rows (so user sees chain names)
    if (allEmpty && blocks && blocks.every && status.Status.every(s => !s.blocks || s.blocks.length === 0)) {
        container.innerHTML = `<div class="tl-empty">
            <div class="tl-empty-icon">⬜⬜⬜</div>
            <div class="tl-empty-title">Block history not yet available</div>
            <div class="tl-empty-sub">Waiting for signing data from Tenderduty</div>
        </div>`
    } else {
        container.innerHTML = html
    }

    if (skeleton) skeleton.style.display = 'none'
    container.style.display = ''
}

// ── Theme ─────────────────────────────────────────────────────────────────

function _syncThemeBtn() {
    const theme = document.documentElement.getAttribute('data-theme') || 'dark'
    const btn   = document.getElementById('theme-btn')
    if (btn) btn.textContent = theme === 'dark' ? '☀ Light' : '🌙 Dark'
}

function toggleTheme() {
    const current = document.documentElement.getAttribute('data-theme') || 'dark'
    const next    = current === 'dark' ? 'light' : 'dark'
    document.documentElement.setAttribute('data-theme', next)
    localStorage.setItem('rn-theme', next)
    _syncThemeBtn()
}

// Kept for any legacy calls (lightMode was the old function name)
function lightMode() { toggleTheme() }

// Sync button label once DOM is ready
document.addEventListener('DOMContentLoaded', _syncThemeBtn)

// DOM-based block history timeline (replaces canvas approach)
let isDark = true

// Block status → color mapping
const TL_COLOR = {
    4: '#3b82f6',  // proposer   – blue
    3: '#10b981',  // signed     – emerald
    2: '#f97316',  // precommit  – orange
    1: '#a855f7',  // prevote    – violet
    0: '#ef4444',  // missed     – red
}
// no-data handled by CSS var --tl-nodata

function legend() {
    // Legend is static HTML in index.html — nothing to render here
}

function drawSeries(status) {
    const container = document.getElementById('timelineRows')
    if (!container || !status || !status.Status) return

    // How many bars fit in the available width
    const nameW = 155 + 10 + 36   // tl-name width + gap + card padding
    const barW  = 5
    const gap   = 1
    const avail = Math.max(0, container.offsetWidth - nameW)
    const slots = Math.max(20, Math.floor(avail / (barW + gap)))

    let html = ''
    for (const s of status.Status) {
        const raw    = s.blocks || []
        const slice  = raw.length > slots ? raw.slice(raw.length - slots) : raw
        let   bars   = ''
        for (const b of slice) {
            const c = TL_COLOR[b]
            bars += c
                ? `<span class="tl-bar" style="--bc:${c}"></span>`
                : `<span class="tl-bar"></span>`
        }
        html += `<div class="tl-row">` +
            `<span class="tl-name" title="${_.escape(s.name)}">${_.escape(s.name)}</span>` +
            `<div class="tl-bars">${bars}</div>` +
            `</div>`
    }
    container.innerHTML = html
}

function lightMode() {
    isDark = !isDark
    document.body.classList.toggle('light', !isDark)
    const btn = document.getElementById('theme-btn')
    if (btn) btn.textContent = isDark ? '☀ Light' : '🌙 Dark'
}

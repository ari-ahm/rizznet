# Rizznet

**Rizznet** is a high-performance, modular Proxy Pipeline built in Go. It automates the lifecycle of censorship evasion proxies: **Collection → Validation → Optimization → Publishing**.

Unlike simple scrapers, Rizznet uses **Simulated Annealing** and a **History Engine** to probabilistically find the fastest proxies within a specific data budget, rather than blindly testing thousands of dead links.

## 🚀 Features

*   **Multi-Source Collection:** Scrapes from Telegram (using a real Userbot client) and HTTP sources.
*   **Smart Parsing:** Supports VMess, VLESS (Reality/Vision), Trojan, Shadowsocks (SIP002), WireGuard, and Hysteria2.
*   **Identity Fingerprinting:** Automatically detects and merges duplicate proxies even if the URL parameters (like `fp` or `sni`) differ.
*   **Optimization Engine:**
    *   **History Tracking:** Remembers proxy performance and penalizes dead nodes.
    *   **Simulated Annealing:** Finds the optimal set of proxies for specific categories (Speed, Clean IP, Specific ISP) without wasting bandwidth.
    *   **Data Budget:** Strict controls on how much data the tester consumes (e.g., "Use max 50MB for testing").
*   **Xray-Core Integration:** Uses the official Xray core as a library for accurate, real-world connection testing.
*   **Flexible Publishing:** Exports subscriptions to Stdout or commits directly to a **GitHub Repository**.

## ⚡ Bootstrapping (Quick Start)

If you have just downloaded a release, follow this guide to populate your database for the first time.

**1. Download & Extract**
Download the latest release for your OS. The archive already includes the `config.yaml` and necessary GeoIP databases. You do **not** need to rename files or download anything else.

**2. First Collection**
You face a "chicken-and-egg" problem: you need proxies to scrape safely, but you don't have any yet.
Run the collector bypassing the internal proxy system:

```bash
./rizznet collect --no-proxy
```

*Note: If this fails (e.g., your ISP blocks the source URLs), start your own local proxy (like v2rayN/NekoBox) on the port defined in `config.yaml` (default: `10808`) and run the command again without the flag:*
```bash
# Only if --no-proxy failed and you have a local proxy running on port 10808
./rizznet collect
```

**3. Test & Optimize**
Run the engine to find working proxies. This will ping candidates and run speed tests.

```bash
./rizznet test
```

**4. Publish**
Output the results to your terminal (or other configured publishers).

```bash
./rizznet publish
```

*After this baseline setup, you can edit `config.yaml` to add Telegram API credentials, GitHub tokens, or change the logic.*

## 🛠️ Installation (From Source)

If you prefer to build from source instead of using the pre-compiled releases:

**Prerequisites:**
*   Go 1.25+
*   Make

```bash
# Clone the repository
git clone https://github.com/ari-ahm/rizznet.git
cd rizznet

# Build the binary and download GeoIP data
make build
make update-geoip

# Create config
cp config.yaml.example config.yaml
```

## ⚙️ Configuration

Rizznet relies on `config.yaml`.
*   **Releases:** This file is included and pre-configured with defaults.
*   **Manual Build:** Copy `config.yaml.example` to `config.yaml`.

Key settings to look at:
*   `system_proxy.fallback`: The local proxy port Rizznet uses if it can't find a working proxy in its own DB (Default: `10808`).
*   `collectors`: Add your Telegram API ID/Hash here to enable the Telegram scraper.
*   `publishers`: Add your GitHub Token here to auto-commit subscriptions.

## 🖥️ Usage

### 1. Collect
Scrape proxies from defined sources.
```bash
# Run all collectors
./rizznet collect

# Run specific collectors with overrides
./rizznet collect telegram_channels --param limit=100
```

### 2. Test & Optimize
Run the annealing engine to find the best proxies.

```bash
# Standard Run (Global Health Check -> Annealing)
./rizznet test

# Fast Mode (Skip Global Check, verify only candidates selected by History)
./rizznet test --fast --budget 50

# Log to file instead of console (clean UI)
./rizznet test --fast --log-file rizznet.log
```

**Flags:**
*   `--fast`: Bypasses the initial mass-health-check. Relies on historical data to pick candidates. Highly recommended for daily cron jobs.
*   `--budget [int]`: Override the data budget (MB).
*   `--workers [int]`: Override the number of concurrent workers.

### 3. Publish
Generate subscriptions.

```bash
# Publish to all configured targets (Stdout, GitHub, etc.)
./rizznet publish

# Publish specifically to stdout with verbose logging
./rizznet publish stdout -v
```

## 🏗️ Architecture

1.  **Collector Layer:** Fetches raw text/links and normalizes them into a `Profile` struct. Calculates a unique `Hash` to prevent duplicates.
2.  **Database:** Stores proxies and their `PerformanceHistory` (Composite key: ProxyID + ISP).
3.  **Heuristic Tester:**
    *   *Inbound Check:* Determines the ISP of the proxy server.
    *   *Outbound Check:* Determines the ISP/IP seen by the target website.
    *   *Speed Test:* Measures download speed against Cloudflare/Google.
4.  **Annealer:** A probabilistic algorithm that selects proxies based on their history score and category weight, verifies them, and fills "Buckets" (e.g., Top 20 Fast, Top 20 Clean).
5.  **Publisher:** Formats the survivors into subscriptions (Base64/Plain) with informative remarks (e.g., `🇩🇪 DE clean|speed`) and uploads them.

## ⚠️ Disclaimer

This tool is for educational and research purposes only. The user is responsible for ensuring that the collection and use of proxies comply with local laws and terms of service of the target platforms.


## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

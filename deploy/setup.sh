#!/bin/bash
# First-time server setup. Run once as root.
# Usage: bash setup.sh

set -e

BOT_DIR=/opt/hiddify-bot

echo "→ Creating bot directory..."
mkdir -p "$BOT_DIR/data"

echo "→ Installing systemd service..."
cp hiddify-bot.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable hiddify-bot

echo ""
echo "✅ Setup complete."
echo ""
echo "Next steps:"
echo "  1. Copy your .env file to $BOT_DIR/.env"
echo "  2. Deploy the bot: push to main branch (GitHub Actions will handle the rest)"
echo "  3. Check status: systemctl status hiddify-bot"
echo "  4. View logs:    journalctl -u hiddify-bot -f"

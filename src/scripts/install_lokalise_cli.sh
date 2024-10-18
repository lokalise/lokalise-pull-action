#!/bin/bash

if ! command -v lokalise2 >/dev/null 2>&1; then
    echo "Installing Lokalise CLI..."
    curl -sfL https://raw.githubusercontent.com/lokalise/lokalise-cli-2-go/master/install.sh | sh || {
        echo "Failed to install Lokalise CLI"
        exit 1
    }
else
    echo "Lokalise CLI is already installed, skipping installation."
fi
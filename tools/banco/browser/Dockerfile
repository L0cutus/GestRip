# Immagine del livello browser del banco (slice 040a, PIANO §4.3): Playwright con il Chromium della
# stessa versione dello standalone (browser/Dockerfile a go-baseline). linux/amd64, base fissata per
# digest; il pacchetto npm e il browser della base devono avere la stessa versione. Si costruisce con
# `banco immagine-browser`, che mette l'etichetta grgo.banco.impronta (impronta di questo file, di
# package.json, del lock e di entrypoint.sh), controllata da `banco esegui`.
FROM mcr.microsoft.com/playwright:v1.58.2-noble@sha256:65cefd09a5e943921ecd3a6e5414c603db2eb161e9eb48f2e2ccc63486dc7dc0
ENV NODE_ENV=production PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 HOME=/tmp/browser-home
WORKDIR /gate
# certutil, per mettere la CA dell'istanza sintetica nel database NSS di Chromium. Versione fissata:
# quella della release di noble, la stessa di libnss3 nella base (2:3.98-1build1), che il pocket
# della release non toglie mai; così non si aggiorna nemmeno libnss3.
RUN apt-get update && apt-get install --yes --no-install-recommends libnss3-tools=2:3.98-1build1 \
    && rm -rf /var/lib/apt/lists/*
COPY package.json package-lock.json ./
# Solo le versioni e le impronte del lock, senza script d'installazione né download di browser.
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY entrypoint.sh ./
USER 1000:1000
ENTRYPOINT ["/bin/sh", "/gate/entrypoint.sh"]

FROM n8nio/n8n:2.1.4-arm64

# Copiar el nodo personalizado
COPY whatsmeow-bundle /home/node/.n8n/custom/n8n-nodes-whatsmeow

# Instalar las dependencias del nodo
USER root
RUN cd /home/node/.n8n/custom/n8n-nodes-whatsmeow && npm install --omit=dev
USER node

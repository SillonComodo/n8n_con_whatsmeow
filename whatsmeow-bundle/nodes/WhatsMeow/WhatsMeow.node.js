"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.WhatsMeow = void 0;
const n8n_workflow_1 = require("n8n-workflow");

// URL base del servidor WhatsMeow (desde variable de entorno o default)
const WHATSMEOW_API_URL = process.env.WHATSMEOW_API_URL || 'http://whatsmeow:3100';

/**
 * Construye el nombre de sesión según el scope seleccionado
 */
function buildSessionName(scope, workflowId, customName) {
    switch (scope) {
        case 'workflow':
            return `wf-${workflowId || 'unknown'}`;
        case 'custom':
            const safe = customName ? customName.replace(/[^a-zA-Z0-9._-]/g, '_') : 'custom';
            return safe;
        case 'global':
        default:
            return 'global';
    }
}

/**
 * Normaliza número de teléfono (quita +, espacios, guiones)
 */
function normalizePhone(phoneNumber) {
    const trimmed = phoneNumber.trim();
    if (!trimmed) {
        throw new Error('Phone number is required');
    }
    return trimmed.replace(/[^0-9]/g, '');
}

/**
 * Convierte URL externa a URL interna de Docker
 * Esto permite que el webhook funcione sin importar desde qué IP accedas
 */
function convertToInternalUrl(externalUrl) {
    if (!externalUrl) return externalUrl;
    
    // Reemplazar cualquier host:5678 por n8n:5678 (red interna Docker)
    // Soporta: localhost, 192.168.x.x, 100.x.x.x (Tailscale), cualquier IP
    return externalUrl.replace(
        /http:\/\/[^:\/]+:5678/,
        'http://n8n:5678'
    );
}

/**
 * Obtiene los datos binarios como base64 de forma segura
 * Maneja diferentes formatos que n8n puede usar
 */
async function getBinaryDataAsBase64(context, itemIndex, binaryPropertyName) {
    const binaryData = context.helpers.assertBinaryData(itemIndex, binaryPropertyName);
    
    // Intentar obtener el buffer usando el helper de n8n (método preferido)
    try {
        const buffer = await context.helpers.getBinaryDataBuffer(itemIndex, binaryPropertyName);
        return {
            base64: buffer.toString('base64'),
            mimeType: binaryData.mimeType,
            fileName: binaryData.fileName,
            fileSize: buffer.length
        };
    } catch (e) {
        // Fallback: si el helper falla, intentar usar el campo data directamente
        if (binaryData.data) {
            // Verificar si ya es base64 válido
            const base64Regex = /^[A-Za-z0-9+/=]+$/;
            if (base64Regex.test(binaryData.data)) {
                return {
                    base64: binaryData.data,
                    mimeType: binaryData.mimeType,
                    fileName: binaryData.fileName,
                    fileSize: binaryData.fileSize
                };
            }
        }
        throw new Error(`Could not extract binary data from property "${binaryPropertyName}": ${e.message}`);
    }
}

/**
 * Hace una request HTTP al servidor WhatsMeow
 */
async function apiRequest(method, endpoint, body = null) {
    const url = `${WHATSMEOW_API_URL}${endpoint}`;
    const options = {
        method,
        headers: {
            'Content-Type': 'application/json',
        },
    };
    
    if (body) {
        options.body = JSON.stringify(body);
    }
    
    const response = await fetch(url, options);
    const data = await response.json();
    
    if (!data.success) {
        throw new Error(data.data || data.error || 'API request failed');
    }
    
    return data.data;
}

class WhatsMeow {
    constructor() {
        this.description = {
            displayName: 'WhatsMeow',
            name: 'whatsMeow',
            icon: 'file:whatsapp.svg',
            group: ['transform'],
            version: 1,
            subtitle: '={{$parameter["operation"]}}',
            description: 'Interact with WhatsApp through WhatsMeow API Server',
            defaults: {
                name: 'WhatsMeow',
            },
            inputs: [n8n_workflow_1.NodeConnectionTypes.Main],
            outputs: [n8n_workflow_1.NodeConnectionTypes.Main],
            properties: [
                {
                    displayName: 'Operation',
                    name: 'operation',
                    type: 'options',
                    options: [
                        { name: 'Connect / Show QR', value: 'connectQr' },
                        { name: 'Send Text Message', value: 'sendMessage' },
                        { name: 'Send Media', value: 'sendMedia' },
                        { name: 'Download Media', value: 'downloadMedia' },
                        { name: 'Get Message History', value: 'getHistory' },
                        { name: 'Check Status', value: 'checkStatus' },
                        { name: 'Configure Webhook', value: 'configureWebhook' },
                        { name: 'List Groups', value: 'listGroups' },
                        { name: 'Get Group Info', value: 'getGroupInfo' },
                        { name: 'Create Group', value: 'createGroup' },
                        { name: 'Leave Group', value: 'leaveGroup' },
                        { name: 'Get Group Invite Link', value: 'getGroupInviteLink' },
                        { name: 'List Sessions', value: 'listSessions' },
                        { name: 'Delete Session', value: 'deleteSession' },
                        { name: 'Logout', value: 'logout' },
                    ],
                    default: 'connectQr',
                    description: 'Action to perform with the WhatsApp API',
                },
                // === SESSION SCOPE ===
                {
                    displayName: 'Session Scope',
                    name: 'sessionScope',
                    type: 'options',
                    options: [
                        { name: 'Global (shared)', value: 'global' },
                        { name: 'Workflow (isolated)', value: 'workflow' },
                        { name: 'Custom Name', value: 'custom' },
                    ],
                    default: 'global',
                    description: 'Which session to use',
                    displayOptions: {
                        hide: {
                            operation: ['listSessions'],
                        },
                    },
                },
                {
                    displayName: 'Custom Session Name',
                    name: 'sessionName',
                    type: 'string',
                    default: '',
                    description: 'Custom session name',
                    displayOptions: {
                        show: {
                            sessionScope: ['custom'],
                        },
                    },
                },
                // === MESSAGE TARGET ===
                {
                    displayName: 'Target Type',
                    name: 'targetType',
                    type: 'options',
                    options: [
                        { name: 'Phone Number', value: 'phone' },
                        { name: 'Group JID', value: 'group' },
                    ],
                    default: 'phone',
                    displayOptions: {
                        show: {
                            operation: ['sendMessage', 'sendMedia'],
                        },
                    },
                },
                {
                    displayName: 'Phone Number',
                    name: 'phoneNumber',
                    type: 'string',
                    default: '',
                    description: 'Destination phone number (with country code, e.g. 5491123456789)',
                    displayOptions: {
                        show: {
                            operation: ['sendMessage', 'sendMedia'],
                            targetType: ['phone'],
                        },
                    },
                },
                {
                    displayName: 'Group JID',
                    name: 'groupJid',
                    type: 'string',
                    default: '',
                    description: 'Group JID (e.g. 123456789012345678@g.us)',
                    displayOptions: {
                        show: {
                            operation: ['sendMessage', 'sendMedia', 'getGroupInfo', 'leaveGroup', 'getGroupInviteLink'],
                            targetType: ['group'],
                        },
                    },
                },
                // === TEXT MESSAGE ===
                {
                    displayName: 'Message',
                    name: 'message',
                    type: 'string',
                    typeOptions: {
                        rows: 3,
                    },
                    default: '',
                    description: 'Text message to send',
                    displayOptions: {
                        show: {
                            operation: ['sendMessage'],
                        },
                    },
                },
                // === MEDIA ===
                {
                    displayName: 'Media Type',
                    name: 'mediaType',
                    type: 'options',
                    options: [
                        { name: 'Image', value: 'image' },
                        { name: 'Video', value: 'video' },
                        { name: 'Audio', value: 'audio' },
                        { name: 'Document', value: 'document' },
                        { name: 'Sticker', value: 'sticker' },
                    ],
                    default: 'image',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                        },
                    },
                },
                {
                    displayName: 'Send as Voice Message (PTT)',
                    name: 'ptt',
                    type: 'boolean',
                    default: false,
                    description: 'Send audio as voice message (like recording) instead of audio file. Best format: OGG Opus',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                            mediaType: ['audio'],
                            sendMultiple: [false],
                        },
                    },
                },
                {
                    displayName: 'Send Multiple Files',
                    name: 'sendMultiple',
                    type: 'boolean',
                    default: false,
                    description: 'Send multiple files in sequence',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                        },
                    },
                },
                {
                    displayName: 'Media Source',
                    name: 'mediaSource',
                    type: 'options',
                    options: [
                        { name: 'URL (Web)', value: 'url' },
                        { name: 'Local File (/files/...)', value: 'local' },
                        { name: 'Binary Data', value: 'binary' },
                    ],
                    default: 'url',
                    description: 'Source of the media file. Local files must be in /home/bryan/docker/n8n/local-files/',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                            sendMultiple: [false],
                        },
                    },
                },
                {
                    displayName: 'Media URL',
                    name: 'mediaUrl',
                    type: 'string',
                    default: '',
                    description: 'URL of the media file to send (https://...)',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                            mediaSource: ['url'],
                            sendMultiple: [false],
                        },
                    },
                },
                {
                    displayName: 'Local File Path',
                    name: 'localFilePath',
                    type: 'string',
                    default: '',
                    placeholder: '/files/imagen.jpg',
                    description: 'Path to local file. Files must be in /home/bryan/docker/n8n/local-files/ on the host. Use /files/filename.ext',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                            mediaSource: ['local'],
                            sendMultiple: [false],
                        },
                    },
                },
                {
                    displayName: 'Binary Property',
                    name: 'binaryProperty',
                    type: 'string',
                    default: 'data',
                    description: 'Name of the binary property containing the media',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                            mediaSource: ['binary'],
                            sendMultiple: [false],
                        },
                    },
                },
                {
                    displayName: 'Files',
                    name: 'mediaFiles',
                    type: 'fixedCollection',
                    typeOptions: {
                        multipleValues: true,
                    },
                    default: {},
                    placeholder: 'Add File',
                    description: 'List of files to send',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                            sendMultiple: [true],
                        },
                    },
                    options: [
                        {
                            name: 'files',
                            displayName: 'File',
                            values: [
                                {
                                    displayName: 'Source',
                                    name: 'source',
                                    type: 'options',
                                    options: [
                                        { name: 'URL (Web)', value: 'url' },
                                        { name: 'Local File', value: 'local' },
                                    ],
                                    default: 'url',
                                },
                                {
                                    displayName: 'URL',
                                    name: 'url',
                                    type: 'string',
                                    default: '',
                                    displayOptions: {
                                        show: {
                                            source: ['url'],
                                        },
                                    },
                                },
                                {
                                    displayName: 'Local Path',
                                    name: 'localPath',
                                    type: 'string',
                                    default: '',
                                    placeholder: '/files/imagen.jpg',
                                    displayOptions: {
                                        show: {
                                            source: ['local'],
                                        },
                                    },
                                },
                                {
                                    displayName: 'Caption',
                                    name: 'caption',
                                    type: 'string',
                                    typeOptions: {
                                        rows: 4,
                                    },
                                    default: '',
                                },
                                {
                                    displayName: 'Filename (for documents)',
                                    name: 'fileName',
                                    type: 'string',
                                    default: '',
                                },
                            ],
                        },
                    ],
                },
                {
                    displayName: 'Caption',
                    name: 'caption',
                    type: 'string',
                    typeOptions: {
                        rows: 4,
                    },
                    default: '',
                    description: 'Caption for the media (optional). Supports multiple lines.',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                            sendMultiple: [false],
                            mediaType: ['image', 'video', 'document'],
                        },
                    },
                },
                {
                    displayName: 'Sticker Info',
                    name: 'stickerInfo',
                    type: 'notice',
                    default: '',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                            mediaType: ['sticker'],
                        },
                    },
                },
                {
                    displayName: 'File Name',
                    name: 'fileName',
                    type: 'string',
                    default: '',
                    description: 'File name for documents (optional)',
                    displayOptions: {
                        show: {
                            operation: ['sendMedia'],
                            mediaType: ['document'],
                            sendMultiple: [false],
                        },
                    },
                },
                // === WEBHOOK ===
                {
                    displayName: 'Webhook Action',
                    name: 'webhookAction',
                    type: 'options',
                    options: [
                        { name: 'Get Config', value: 'get' },
                        { name: 'Set Config', value: 'set' },
                        { name: 'Remove Config', value: 'remove' },
                    ],
                    default: 'get',
                    displayOptions: {
                        show: {
                            operation: ['configureWebhook'],
                        },
                    },
                },
                {
                    displayName: 'Webhook URL',
                    name: 'webhookUrl',
                    type: 'string',
                    default: '',
                    description: 'URL to receive incoming messages (POST requests)',
                    displayOptions: {
                        show: {
                            operation: ['configureWebhook'],
                            webhookAction: ['set'],
                        },
                    },
                },
                {
                    displayName: 'Webhook Enabled',
                    name: 'webhookEnabled',
                    type: 'boolean',
                    default: true,
                    displayOptions: {
                        show: {
                            operation: ['configureWebhook'],
                            webhookAction: ['set'],
                        },
                    },
                },
                // === GROUP OPERATIONS ===
                {
                    displayName: 'Group JID',
                    name: 'groupJidParam',
                    type: 'string',
                    default: '',
                    description: 'Group JID for group operations',
                    displayOptions: {
                        show: {
                            operation: ['getGroupInfo', 'leaveGroup', 'getGroupInviteLink'],
                        },
                    },
                },
                // === HISTORY OPERATIONS ===
                {
                    displayName: 'Chat JID',
                    name: 'historyChat',
                    type: 'string',
                    default: '',
                    description: 'Chat JID to get history from (leave empty to list all available chats)',
                    displayOptions: {
                        show: {
                            operation: ['getHistory'],
                        },
                    },
                },
                {
                    displayName: 'Limit',
                    name: 'historyLimit',
                    type: 'number',
                    default: 50,
                    description: 'Maximum number of messages to retrieve (1-1000)',
                    displayOptions: {
                        show: {
                            operation: ['getHistory'],
                        },
                    },
                },
                {
                    displayName: 'Group Name',
                    name: 'groupName',
                    type: 'string',
                    default: '',
                    description: 'Name for the new group',
                    displayOptions: {
                        show: {
                            operation: ['createGroup'],
                        },
                    },
                },
                {
                    displayName: 'Participants',
                    name: 'participants',
                    type: 'string',
                    default: '',
                    description: 'Comma-separated phone numbers to add to the group',
                    displayOptions: {
                        show: {
                            operation: ['createGroup'],
                        },
                    },
                },
                // === DOWNLOAD MEDIA ===
                {
                    displayName: 'Media Cache Key',
                    name: 'mediaCacheKey',
                    type: 'string',
                    default: '={{ $json.media_cache_key }}',
                    description: 'The cache key from the incoming message (from WhatsMeow Trigger)',
                    displayOptions: {
                        show: {
                            operation: ['downloadMedia'],
                        },
                    },
                },
                {
                    displayName: 'Binary Property Name',
                    name: 'binaryPropertyOutput',
                    type: 'string',
                    default: 'data',
                    description: 'Name of the binary property to store the downloaded media',
                    displayOptions: {
                        show: {
                            operation: ['downloadMedia'],
                        },
                    },
                },
            ],
        };
    }

    async execute() {
        var _a;
        const items = this.getInputData();
        const returnData = [];

        for (let i = 0; i < items.length; i++) {
            const operation = this.getNodeParameter('operation', i);
            
            // Para listSessions no necesitamos session name
            if (operation === 'listSessions') {
                try {
                    const sessions = await apiRequest('GET', '/sessions');
                    returnData.push({ 
                        json: { 
                            status: 'success', 
                            sessions, 
                            total: sessions ? sessions.length : 0,
                            api_url: WHATSMEOW_API_URL 
                        } 
                    });
                } catch (error) {
                    throw new n8n_workflow_1.NodeOperationError(this.getNode(), error.message, { itemIndex: i });
                }
                continue;
            }

            // Para otras operaciones, obtener session name
            const sessionScope = this.getNodeParameter('sessionScope', i, 'global');
            const workflowId = (_a = this.getWorkflow().id) !== null && _a !== void 0 ? _a : 'unknown';
            const customName = sessionScope === 'custom' 
                ? this.getNodeParameter('sessionName', i, '') 
                : '';
            const sessionName = buildSessionName(sessionScope, workflowId, customName);

            try {
                switch (operation) {
                    case 'connectQr': {
                        const data = await apiRequest('POST', `/session/${sessionName}/connect`);
                        
                        if (data.status === 'connected' || data.status === 'reconnected') {
                            returnData.push({
                                json: {
                                    status: data.status,
                                    jid: data.jid,
                                    connected: data.connected !== false,
                                    session: sessionName,
                                    message: data.message || 'Already connected'
                                }
                            });
                        } else if (data.status === 'qr_ready') {
                            // Convertir QR data URL a imagen binaria
                            if (data.qr_image) {
                                const base64Data = data.qr_image.replace('data:image/png;base64,', '');
                                const binaryData = Buffer.from(base64Data, 'base64');
                                const binary = await this.helpers.prepareBinaryData(binaryData, 'qr-code.png', 'image/png');
                                
                                returnData.push({
                                    json: {
                                        status: 'qr_ready',
                                        session: sessionName,
                                        instructions: 'Scan the QR code with WhatsApp: Settings > Linked Devices > Link a Device',
                                    },
                                    binary: { qrCode: binary }
                                });
                            } else {
                                returnData.push({
                                    json: {
                                        status: 'qr_ready',
                                        session: sessionName,
                                        qr_ascii: data.qr_ascii,
                                        message: data.message
                                    }
                                });
                            }
                        } else {
                            returnData.push({ json: data });
                        }
                        break;
                    }

                    case 'sendMessage': {
                        const targetType = this.getNodeParameter('targetType', i, 'phone');
                        const message = this.getNodeParameter('message', i);
                        
                        const body = { message };
                        
                        if (targetType === 'group') {
                            body.group_jid = this.getNodeParameter('groupJid', i);
                        } else {
                            body.phone = normalizePhone(this.getNodeParameter('phoneNumber', i));
                        }
                        
                        const data = await apiRequest('POST', `/session/${sessionName}/send`, body);
                        
                        returnData.push({
                            json: {
                                status: 'sent',
                                session: sessionName,
                                target: body.phone || body.group_jid,
                                message_id: data.message_id,
                                timestamp: data.timestamp
                            }
                        });
                        break;
                    }

                    case 'sendMedia': {
                        const targetType = this.getNodeParameter('targetType', i, 'phone');
                        const mediaType = this.getNodeParameter('mediaType', i);
                        const sendMultiple = this.getNodeParameter('sendMultiple', i, false);
                        
                        // Determinar destino
                        let targetPhone = '';
                        let targetGroup = '';
                        if (targetType === 'group') {
                            targetGroup = this.getNodeParameter('groupJid', i);
                        } else {
                            targetPhone = normalizePhone(this.getNodeParameter('phoneNumber', i));
                        }
                        
                        const results = [];
                        
                        if (sendMultiple) {
                            // Enviar múltiples archivos
                            const mediaFiles = this.getNodeParameter('mediaFiles', i, {});
                            const files = mediaFiles.files || [];
                            
                            for (const file of files) {
                                const body = {
                                    media_type: mediaType,
                                    caption: file.caption || '',
                                    file_name: file.fileName || '',
                                };
                                
                                if (targetType === 'group') {
                                    body.group_jid = targetGroup;
                                } else {
                                    body.phone = targetPhone;
                                }
                                
                                // Determinar fuente del archivo
                                if (file.source === 'local') {
                                    body.media_url = file.localPath; // El servidor Go detectará /files/
                                } else {
                                    body.media_url = file.url;
                                }
                                
                                try {
                                    const data = await apiRequest('POST', `/session/${sessionName}/send-media`, body);
                                    results.push({
                                        status: 'sent',
                                        url: body.media_url,
                                        message_id: data.message_id,
                                        timestamp: data.timestamp
                                    });
                                } catch (error) {
                                    results.push({
                                        status: 'error',
                                        url: body.media_url,
                                        error: error.message
                                    });
                                }
                                
                                // Pequeña pausa entre envíos para evitar rate limits
                                await new Promise(resolve => setTimeout(resolve, 500));
                            }
                            
                            returnData.push({
                                json: {
                                    status: 'completed',
                                    session: sessionName,
                                    target: targetPhone || targetGroup,
                                    media_type: mediaType,
                                    files_sent: results.filter(r => r.status === 'sent').length,
                                    files_failed: results.filter(r => r.status === 'error').length,
                                    results: results
                                }
                            });
                        } else {
                            // Enviar un solo archivo
                            const mediaSource = this.getNodeParameter('mediaSource', i);
                            const caption = this.getNodeParameter('caption', i, '');
                            const fileName = this.getNodeParameter('fileName', i, '');
                            
                            // Obtener opción PTT para audio
                            let ptt = false;
                            if (mediaType === 'audio') {
                                ptt = this.getNodeParameter('ptt', i, false);
                            }
                            
                            const body = {
                                media_type: mediaType,
                                caption,
                                file_name: fileName,
                                ptt: ptt, // Para audio: true = mensaje de voz
                            };
                            
                            if (targetType === 'group') {
                                body.group_jid = targetGroup;
                            } else {
                                body.phone = targetPhone;
                            }
                            
                            if (mediaSource === 'url') {
                                body.media_url = this.getNodeParameter('mediaUrl', i);
                            } else if (mediaSource === 'local') {
                                // Archivo local - usar la ruta directamente
                                body.media_url = this.getNodeParameter('localFilePath', i);
                            } else {
                                // Binary data - usar helper seguro para obtener base64
                                const binaryProperty = this.getNodeParameter('binaryProperty', i, 'data');
                                try {
                                    const binaryInfo = await getBinaryDataAsBase64(this, i, binaryProperty);
                                    body.media_base64 = binaryInfo.base64;
                                    if (binaryInfo.mimeType) {
                                        body.mime_type = binaryInfo.mimeType;
                                    }
                                    if (binaryInfo.fileName && !body.file_name) {
                                        body.file_name = binaryInfo.fileName;
                                    }
                                } catch (binaryError) {
                                    throw new Error(`Failed to process binary data from "${binaryProperty}": ${binaryError.message}`);
                                }
                            }
                            
                            const data = await apiRequest('POST', `/session/${sessionName}/send-media`, body);
                            
                            returnData.push({
                                json: {
                                    status: 'sent',
                                    session: sessionName,
                                    target: body.phone || body.group_jid,
                                    media_type: mediaType,
                                    message_id: data.message_id,
                                    timestamp: data.timestamp
                                }
                            });
                        }
                        break;
                    }

                    case 'downloadMedia': {
                        const mediaCacheKey = this.getNodeParameter('mediaCacheKey', i);
                        const binaryPropertyOutput = this.getNodeParameter('binaryPropertyOutput', i, 'data');
                        
                        if (!mediaCacheKey) {
                            throw new Error('Media cache key is required. This should come from a WhatsMeow Trigger message with has_media=true');
                        }
                        
                        // Llamada directa sin usar apiRequest porque download-media tiene respuesta diferente
                        const url = `${WHATSMEOW_API_URL}/session/${sessionName}/download-media`;
                        const response = await fetch(url, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ cache_key: mediaCacheKey })
                        });
                        const data = await response.json();
                        
                        if (!data.success) {
                            throw new Error(data.message || data.data || 'Failed to download media');
                        }
                        
                        // Convertir base64 a Buffer y crear binary data
                        const binaryData = Buffer.from(data.media_base64, 'base64');
                        
                        // Preparar la salida con binary data
                        const newItem = {
                            json: {
                                status: 'downloaded',
                                session: sessionName,
                                media_type: data.media_type,
                                mime_type: data.mime_type,
                                file_name: data.file_name,
                                file_size: data.file_size,
                            },
                            binary: {}
                        };
                        
                        // Agregar los datos binarios usando el helper de n8n
                        newItem.binary[binaryPropertyOutput] = await this.helpers.prepareBinaryData(
                            binaryData,
                            data.file_name,
                            data.mime_type
                        );
                        
                        returnData.push(newItem);
                        break;
                    }

                    case 'getHistory': {
                        const chatJID = this.getNodeParameter('historyChat', i, '');
                        const limit = this.getNodeParameter('historyLimit', i, 50);
                        
                        // Construir URL con query params
                        let url = `/session/${sessionName}/history`;
                        const params = new URLSearchParams();
                        if (chatJID) {
                            params.append('chat', chatJID);
                        }
                        if (limit) {
                            params.append('limit', limit.toString());
                        }
                        const queryString = params.toString();
                        if (queryString) {
                            url += '?' + queryString;
                        }
                        
                        const data = await apiRequest('GET', url);
                        
                        if (chatJID) {
                            // Devolver mensajes del chat específico
                            returnData.push({
                                json: {
                                    status: 'success',
                                    session: sessionName,
                                    chat: chatJID,
                                    messages: data.messages || [],
                                    count: data.count || 0
                                }
                            });
                        } else {
                            // Devolver lista de chats disponibles
                            returnData.push({
                                json: {
                                    status: 'success',
                                    session: sessionName,
                                    chats: data.chats || [],
                                    count: data.count || 0
                                }
                            });
                        }
                        break;
                    }

                    case 'checkStatus': {
                        const data = await apiRequest('GET', `/session/${sessionName}/status`);
                        returnData.push({
                            json: {
                                status: 'success',
                                session: sessionName,
                                jid: data.jid,
                                connected: data.connected,
                                webhook_enabled: data.webhook_enabled
                            }
                        });
                        break;
                    }

                    case 'configureWebhook': {
                        const webhookAction = this.getNodeParameter('webhookAction', i, 'get');
                        
                        if (webhookAction === 'get') {
                            const data = await apiRequest('GET', `/session/${sessionName}/webhook`);
                            returnData.push({
                                json: {
                                    status: 'success',
                                    session: sessionName,
                                    webhook: data
                                }
                            });
                        } else if (webhookAction === 'set') {
                            let webhookUrl = this.getNodeParameter('webhookUrl', i);
                            const webhookEnabled = this.getNodeParameter('webhookEnabled', i, true);
                            
                            // Convertir URL externa a URL interna de Docker
                            // n8n muestra URLs con IP externa (192.168.x.x, 100.x.x.x Tailscale, localhost)
                            // pero whatsmeow necesita usar "n8n" (nombre del servicio Docker)
                            webhookUrl = convertToInternalUrl(webhookUrl);
                            
                            await apiRequest('POST', `/session/${sessionName}/webhook`, {
                                url: webhookUrl,
                                enabled: webhookEnabled
                            });
                            
                            returnData.push({
                                json: {
                                    status: 'configured',
                                    session: sessionName,
                                    webhook_url: webhookUrl,
                                    webhook_enabled: webhookEnabled
                                }
                            });
                        } else if (webhookAction === 'remove') {
                            await apiRequest('DELETE', `/session/${sessionName}/webhook`);
                            returnData.push({
                                json: {
                                    status: 'removed',
                                    session: sessionName,
                                    message: 'Webhook removed'
                                }
                            });
                        }
                        break;
                    }

                    case 'listGroups': {
                        const data = await apiRequest('GET', `/session/${sessionName}/groups`);
                        returnData.push({
                            json: {
                                status: 'success',
                                session: sessionName,
                                groups: data,
                                total: data ? data.length : 0
                            }
                        });
                        break;
                    }

                    case 'getGroupInfo': {
                        const groupJid = this.getNodeParameter('groupJidParam', i);
                        const data = await apiRequest('GET', `/session/${sessionName}/group-info?jid=${encodeURIComponent(groupJid)}`);
                        returnData.push({
                            json: {
                                status: 'success',
                                session: sessionName,
                                group: data
                            }
                        });
                        break;
                    }

                    case 'createGroup': {
                        const groupName = this.getNodeParameter('groupName', i);
                        const participantsStr = this.getNodeParameter('participants', i, '');
                        const participants = participantsStr
                            .split(',')
                            .map(p => p.trim())
                            .filter(p => p);
                        
                        const data = await apiRequest('POST', `/session/${sessionName}/create-group`, {
                            name: groupName,
                            participants
                        });
                        
                        returnData.push({
                            json: {
                                status: 'created',
                                session: sessionName,
                                group: data
                            }
                        });
                        break;
                    }

                    case 'leaveGroup': {
                        const groupJid = this.getNodeParameter('groupJidParam', i);
                        await apiRequest('POST', `/session/${sessionName}/leave-group?jid=${encodeURIComponent(groupJid)}`);
                        returnData.push({
                            json: {
                                status: 'left',
                                session: sessionName,
                                group_jid: groupJid
                            }
                        });
                        break;
                    }

                    case 'getGroupInviteLink': {
                        const groupJid = this.getNodeParameter('groupJidParam', i);
                        const data = await apiRequest('GET', `/session/${sessionName}/group-invite-link?jid=${encodeURIComponent(groupJid)}`);
                        returnData.push({
                            json: {
                                status: 'success',
                                session: sessionName,
                                group_jid: groupJid,
                                invite_link: data.invite_link
                            }
                        });
                        break;
                    }

                    case 'deleteSession': {
                        await apiRequest('DELETE', `/session/${sessionName}`);
                        returnData.push({
                            json: {
                                status: 'deleted',
                                session: sessionName,
                                message: 'Session deleted successfully'
                            }
                        });
                        break;
                    }

                    case 'logout': {
                        await apiRequest('POST', `/session/${sessionName}/logout`);
                        returnData.push({
                            json: {
                                status: 'logged_out',
                                session: sessionName,
                                message: 'Session logged out (but data preserved)'
                            }
                        });
                        break;
                    }

                    default:
                        throw new n8n_workflow_1.NodeOperationError(this.getNode(), `Unsupported operation: ${operation}`, { itemIndex: i });
                }
            } catch (error) {
                throw new n8n_workflow_1.NodeOperationError(this.getNode(), error.message, { itemIndex: i });
            }
        }

        return [returnData];
    }
}

exports.WhatsMeow = WhatsMeow;

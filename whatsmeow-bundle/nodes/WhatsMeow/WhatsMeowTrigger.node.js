"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.WhatsMeowTrigger = void 0;
const n8n_workflow_1 = require("n8n-workflow");

// URL base del servidor WhatsMeow
const WHATSMEOW_API_URL = process.env.WHATSMEOW_API_URL || 'http://whatsmeow:3100';

/**
 * Convierte URL externa a URL interna de Docker
 * Soporta URLs como:
 * - http://192.168.x.x:5678/... -> http://n8n:5678/...
 * - https://xxx.tail91a6a6.ts.net/... -> http://n8n:5678/... (Tailscale)
 * - https://xxx.tailscale.net/... -> http://n8n:5678/... (Tailscale alt)
 */
function convertToInternalUrl(externalUrl) {
    if (!externalUrl) return externalUrl;
    
    // Convertir URLs de Tailscale (HTTPS externo) a interno Docker
    // Ejemplo: https://bryan.tail91a6a6.ts.net/webhook/xxx -> http://n8n:5678/webhook/xxx
    let url = externalUrl.replace(
        /https?:\/\/[^\/]+\.ts\.net\/?/,
        'http://n8n:5678/'
    );
    
    // También manejar URLs con tailscale.net
    url = url.replace(
        /https?:\/\/[^\/]+\.tailscale\.net\/?/,
        'http://n8n:5678/'
    );
    
    // Convertir URLs con IP:puerto
    url = url.replace(
        /https?:\/\/[^:\/]+:5678\/?/,
        'http://n8n:5678/'
    );
    
    // Evitar doble slash después del puerto
    url = url.replace(/n8n:5678\/\//, 'n8n:5678/');
    
    return url;
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
    
    try {
        const response = await fetch(url, options);
        return await response.json();
    } catch (error) {
        console.error(`WhatsMeow API error: ${error.message}`);
        return { success: false, error: error.message };
    }
}

class WhatsMeowTrigger {
    constructor() {
        this.description = {
            displayName: 'WhatsMeow Trigger',
            name: 'whatsMeowTrigger',
            icon: 'file:whatsapp.svg',
            group: ['trigger'],
            version: 1,
            subtitle: '={{$parameter["autoRegister"] ? "Auto: " + ($parameter["sessionScope"] === "custom" ? $parameter["customSessionName"] : $parameter["sessionScope"]) : "Manual webhook"}}',
            description: 'Triggers when a WhatsApp message is received. Auto-registers webhook on activation.',
            defaults: {
                name: 'WhatsMeow Trigger',
            },
            inputs: [],
            outputs: [n8n_workflow_1.NodeConnectionTypes.Main],
            webhooks: [
                {
                    name: 'default',
                    httpMethod: 'POST',
                    responseMode: 'onReceived',
                    path: 'whatsmeow',
                },
            ],
            properties: [
                // ============ AUTO-REGISTER SETTINGS ============
                {
                    displayName: 'Auto-Register Webhook',
                    name: 'autoRegister',
                    type: 'boolean',
                    default: true,
                    description: 'Automatically register this webhook URL with WhatsMeow when workflow activates or test starts. Highly recommended!',
                },
                {
                    displayName: 'Session Scope',
                    name: 'sessionScope',
                    type: 'options',
                    options: [
                        { 
                            name: 'Global (Shared)', 
                            value: 'global',
                            description: 'Use a single shared session called "global"'
                        },
                        { 
                            name: 'Custom Name', 
                            value: 'custom',
                            description: 'Use a custom session name'
                        },
                    ],
                    default: 'global',
                    description: 'Which session to register the webhook for',
                    displayOptions: {
                        show: {
                            autoRegister: [true],
                        },
                    },
                },
                {
                    displayName: 'Custom Session Name',
                    name: 'customSessionName',
                    type: 'string',
                    default: '',
                    placeholder: 'my-bot',
                    description: 'Name of the custom session (e.g., bot1, ventas, soporte)',
                    displayOptions: {
                        show: {
                            autoRegister: [true],
                            sessionScope: ['custom'],
                        },
                    },
                },
                {
                    displayName: 'Register Multiple Sessions',
                    name: 'registerMultiple',
                    type: 'boolean',
                    default: false,
                    description: 'Register webhook for multiple sessions at once',
                    displayOptions: {
                        show: {
                            autoRegister: [true],
                        },
                    },
                },
                {
                    displayName: 'Additional Sessions',
                    name: 'additionalSessions',
                    type: 'string',
                    default: '',
                    placeholder: 'bot1, bot2, ventas',
                    description: 'Comma-separated list of additional session names to register',
                    displayOptions: {
                        show: {
                            autoRegister: [true],
                            registerMultiple: [true],
                        },
                    },
                },

                // ============ MESSAGE BATCHING ============
                {
                    displayName: 'Message Batching',
                    name: 'enableBatching',
                    type: 'boolean',
                    default: false,
                    description: 'Wait for additional messages from the same sender before triggering. Useful when users send multiple quick messages like "Hola" then "¿Cómo estás?"',
                    displayOptions: {
                        show: {
                            autoRegister: [true],
                        },
                    },
                },
                {
                    displayName: 'Batch Wait Time (seconds)',
                    name: 'batchDelay',
                    type: 'number',
                    default: 3,
                    description: 'How many seconds to wait for additional messages from the same sender before triggering (1-30 seconds)',
                    typeOptions: {
                        minValue: 1,
                        maxValue: 30,
                    },
                    displayOptions: {
                        show: {
                            autoRegister: [true],
                            enableBatching: [true],
                        },
                    },
                },
                
                // ============ FILTER SETTINGS ============
                {
                    displayName: 'Filter by Session',
                    name: 'filterSession',
                    type: 'boolean',
                    default: false,
                    description: 'Only trigger for messages from a specific session',
                },
                {
                    displayName: 'Session Name Filter',
                    name: 'sessionName',
                    type: 'string',
                    default: 'global',
                    description: 'Session name to filter (only process messages from this session)',
                    displayOptions: {
                        show: {
                            filterSession: [true],
                        },
                    },
                },
                {
                    displayName: 'Filter by Message Type',
                    name: 'filterType',
                    type: 'boolean',
                    default: false,
                    description: 'Only trigger for specific message types',
                },
                {
                    displayName: 'Message Types',
                    name: 'messageTypes',
                    type: 'multiOptions',
                    options: [
                        { name: 'Text', value: 'text' },
                        { name: 'Media (Image/Video/Audio/Document)', value: 'media' },
                        { name: 'Sticker', value: 'sticker' },
                        { name: 'Location', value: 'location' },
                        { name: 'Contact', value: 'contact' },
                    ],
                    default: ['text'],
                    displayOptions: {
                        show: {
                            filterType: [true],
                        },
                    },
                },
                {
                    displayName: 'Filter by Chat Type',
                    name: 'filterChatType',
                    type: 'boolean',
                    default: false,
                    description: 'Only trigger for messages from groups or private chats',
                },
                {
                    displayName: 'Chat Type',
                    name: 'chatType',
                    type: 'options',
                    options: [
                        { name: 'Private Chat Only', value: 'private' },
                        { name: 'Group Only', value: 'group' },
                    ],
                    default: 'private',
                    displayOptions: {
                        show: {
                            filterChatType: [true],
                        },
                    },
                },
            ],
        };
    }

    /**
     * Called when the workflow is activated (production mode)
     * or when test mode starts
     */
    webhookMethods = {
        default: {
            async checkExists() {
                // Always return false to ensure create() runs
                return false;
            },
            
            async create() {
                const autoRegister = this.getNodeParameter('autoRegister', true);
                if (!autoRegister) {
                    console.log('[WhatsMeowTrigger] Auto-register disabled, skipping webhook registration');
                    return true;
                }

                // Get the webhook URL that n8n generated for this trigger
                const webhookData = this.getNodeWebhookUrl('default');
                const internalUrl = convertToInternalUrl(webhookData);
                
                console.log(`[WhatsMeowTrigger] Registering webhook: ${internalUrl}`);

                // Build list of sessions to register
                const sessions = [];
                
                const sessionScope = this.getNodeParameter('sessionScope', 'global');
                if (sessionScope === 'custom') {
                    const customName = this.getNodeParameter('customSessionName', '');
                    if (customName) {
                        sessions.push(customName.trim());
                    } else {
                        sessions.push('global');
                    }
                } else {
                    sessions.push('global');
                }

                // Add additional sessions if configured
                const registerMultiple = this.getNodeParameter('registerMultiple', false);
                if (registerMultiple) {
                    const additionalSessions = this.getNodeParameter('additionalSessions', '');
                    if (additionalSessions) {
                        const extras = additionalSessions.split(',').map(s => s.trim()).filter(s => s);
                        sessions.push(...extras);
                    }
                }

                // Get batching configuration
                const enableBatching = this.getNodeParameter('enableBatching', false);
                const batchDelay = enableBatching ? this.getNodeParameter('batchDelay', 3) : 0;

                // Store registered sessions for cleanup on delete
                const registeredWebhooks = [];

                // Register webhook for each session
                for (const sessionName of sessions) {
                    console.log(`[WhatsMeowTrigger] Registering webhook for session: ${sessionName}${batchDelay > 0 ? ` (batch delay: ${batchDelay}s)` : ''}`);
                    const result = await apiRequest('POST', `/session/${sessionName}/webhook`, {
                        url: internalUrl,
                        enabled: true,
                        batch_delay: batchDelay
                    });
                    
                    if (result.success) {
                        console.log(`[WhatsMeowTrigger] ✓ Webhook registered for session: ${sessionName}`);
                        registeredWebhooks.push({ session: sessionName, url: internalUrl });
                    } else {
                        console.error(`[WhatsMeowTrigger] ✗ Failed to register webhook for ${sessionName}: ${result.error || result.data}`);
                    }
                }

                // Store webhook info in static data for cleanup
                const staticData = this.getWorkflowStaticData('node');
                staticData.registeredWebhooks = registeredWebhooks;

                return true;
            },
            
            async delete() {
                const autoRegister = this.getNodeParameter('autoRegister', true);
                if (!autoRegister) {
                    return true;
                }

                // Get the webhook URL to remove
                const webhookData = this.getNodeWebhookUrl('default');
                const internalUrl = convertToInternalUrl(webhookData);
                
                console.log(`[WhatsMeowTrigger] Removing webhook: ${internalUrl}`);

                // Get sessions from static data or rebuild from parameters
                const staticData = this.getWorkflowStaticData('node');
                let webhooksToRemove = staticData.registeredWebhooks || [];
                
                // If no stored webhooks, rebuild from parameters
                if (webhooksToRemove.length === 0) {
                    const sessions = [];
                    const sessionScope = this.getNodeParameter('sessionScope', 'global');
                    if (sessionScope === 'custom') {
                        const customName = this.getNodeParameter('customSessionName', '');
                        sessions.push(customName ? customName.trim() : 'global');
                    } else {
                        sessions.push('global');
                    }
                    
                    const registerMultiple = this.getNodeParameter('registerMultiple', false);
                    if (registerMultiple) {
                        const additionalSessions = this.getNodeParameter('additionalSessions', '');
                        if (additionalSessions) {
                            const extras = additionalSessions.split(',').map(s => s.trim()).filter(s => s);
                            sessions.push(...extras);
                        }
                    }
                    
                    webhooksToRemove = sessions.map(s => ({ session: s, url: internalUrl }));
                }

                // Remove webhook from each session
                for (const webhook of webhooksToRemove) {
                    console.log(`[WhatsMeowTrigger] Removing webhook from session: ${webhook.session}`);
                    const result = await apiRequest('DELETE', `/session/${webhook.session}/webhook?url=${encodeURIComponent(webhook.url)}`);
                    
                    if (result.success) {
                        console.log(`[WhatsMeowTrigger] ✓ Webhook removed from session: ${webhook.session}`);
                    } else {
                        console.error(`[WhatsMeowTrigger] ✗ Failed to remove webhook from ${webhook.session}: ${result.error || result.data}`);
                    }
                }

                // Clear stored data
                staticData.registeredWebhooks = [];
                
                return true;
            },
        },
    };

    async webhook() {
        const req = this.getRequestObject();
        const body = req.body;

        // Validate that this is a valid WhatsMeow webhook payload
        if (!body || typeof body !== 'object') {
            return {
                workflowData: [],
            };
        }

        // Check required fields
        if (!body.message_id && !body.from && !body.type) {
            // Not a valid WhatsMeow message payload, ignore
            return {
                workflowData: [],
            };
        }

        // Filter by session
        const filterSession = this.getNodeParameter('filterSession', false);
        if (filterSession) {
            const sessionName = this.getNodeParameter('sessionName', 'global');
            if (body.session_name !== sessionName) {
                return {
                    workflowData: [],
                };
            }
        }

        // Filter by message type
        const filterType = this.getNodeParameter('filterType', false);
        if (filterType) {
            const messageTypes = this.getNodeParameter('messageTypes', ['text']);
            if (!messageTypes.includes(body.type)) {
                return {
                    workflowData: [],
                };
            }
        }

        // Filter by chat type
        const filterChatType = this.getNodeParameter('filterChatType', false);
        if (filterChatType) {
            const chatType = this.getNodeParameter('chatType', 'private');
            if (chatType === 'private' && body.is_group) {
                return {
                    workflowData: [],
                };
            }
            if (chatType === 'group' && !body.is_group) {
                return {
                    workflowData: [],
                };
            }
        }

        // Build the output data
        const outputData = {
            // Core message info
            session: body.session_name || '',
            messageId: body.message_id || '',
            timestamp: body.timestamp || 0,
            
            // Sender info - PHONE es el campo principal para el número
            phone: body.phone || body.from || '',
            from: body.from || '',
            fromJid: body.from_jid || '',
            fromName: body.from_name || '',
            
            // Chat info
            chat: body.chat || '',
            chatJid: body.chat_jid || '',
            chatName: body.chat_name || '',
            isGroup: body.is_group || false,
            
            // Message content
            type: body.type || 'unknown',
            text: body.text || '',
            caption: body.caption || '',
            
            // Media info (if applicable)
            mediaType: body.media_type || '',
            mimeType: body.mime_type || '',
            fileName: body.file_name || '',
            fileSize: body.file_size || 0,
            hasMedia: body.has_media || false,
            media_cache_key: body.media_cache_key || '',
            
            // Reply context (if applicable)
            quotedId: body.quoted_id || '',
            quotedText: body.quoted_text || '',
            
            // Mentions (if applicable)
            mentions: body.mentions || [],
            
            // Raw data for advanced users
            raw: body.raw || {},
            
            // Full original payload
            _original: body,
        };

        return {
            workflowData: [
                [{ json: outputData }],
            ],
        };
    }
}

exports.WhatsMeowTrigger = WhatsMeowTrigger;

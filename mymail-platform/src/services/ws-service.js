const WebSocket = require('ws');
const jwt = require('jsonwebtoken');
const config = require('../config');

const AUTH_TIMEOUT_MS = 10000; // 10 seconds to authenticate
const HEARTBEAT_INTERVAL_MS = 30000; // 30 seconds ping/pong

class WsService {
  constructor() {
    this.clients = new Map(); // userId -> Set<ws>
  }

  init(server) {
    this.wss = new WebSocket.Server({ server, path: '/ws' });

    // Heartbeat interval - ping all clients every 30s
    this._heartbeatTimer = setInterval(() => {
      this.wss.clients.forEach((ws) => {
        if (ws.isAlive === false) {
          return ws.terminate();
        }
        ws.isAlive = false;
        ws.ping();
      });
    }, HEARTBEAT_INTERVAL_MS);

    this.wss.on('close', () => {
      clearInterval(this._heartbeatTimer);
    });

    this.wss.on('connection', (ws, req) => {
      // Set up auth timeout - close if no token within 10s
      let authenticated = false;
      const authTimer = setTimeout(() => {
        if (!authenticated) {
          ws.close(4003, 'Authentication timeout');
        }
      }, AUTH_TIMEOUT_MS);

      // Authenticate via Sec-WebSocket-Protocol header (avoids token in URL/logs)
      const protocols = req.headers['sec-websocket-protocol'];
      const token = protocols
        ? protocols.split(',').map(s => s.trim()).find(s => s.startsWith('auth.'))?.slice(5)
        : null;

      if (!token) {
        clearTimeout(authTimer);
        ws.close(4001, 'Missing token');
        return;
      }

      try {
        const decoded = jwt.verify(token, config.jwt.secret);
        const userId = decoded.id;
        authenticated = true;
        clearTimeout(authTimer);

        // Mark alive for heartbeat
        ws.isAlive = true;
        ws.on('pong', () => {
          ws.isAlive = true;
        });

        if (!this.clients.has(userId)) {
          this.clients.set(userId, new Set());
        }
        this.clients.get(userId).add(ws);

        ws.on('close', () => {
          const userClients = this.clients.get(userId);
          if (userClients) {
            userClients.delete(ws);
            if (userClients.size === 0) {
              this.clients.delete(userId);
            }
          }
        });

        ws.on('error', () => {
          ws.close();
        });

        // Send welcome
        ws.send(JSON.stringify({ type: 'connected', message: 'WebSocket 已连接' }));
      } catch (err) {
        clearTimeout(authTimer);
        ws.close(4002, 'Invalid token');
      }
    });
  }

  notifyNewMail(userId, mailInfo) {
    const clients = this.clients.get(userId);
    if (!clients) return;

    const message = JSON.stringify({
      type: 'new_mail',
      data: mailInfo,
    });

    for (const ws of clients) {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(message);
      }
    }
  }

  shutdown() {
    if (this._heartbeatTimer) {
      clearInterval(this._heartbeatTimer);
      this._heartbeatTimer = null;
    }
    if (this.wss) {
      for (const ws of this.wss.clients) {
        ws.close();
      }
      this.wss.close();
    }
    this.clients.clear();
  }
}

module.exports = new WsService();

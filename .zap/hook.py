"""ZAP baseline hook that replays the requests written by seed.sh, so the passive rules also see the API responses."""
import json
from urllib.parse import urlparse


def zap_started(zap, target):
    with open('/zap/wrk/requests.json') as handle:
        items = json.load(handle)

    base = target.rstrip('/')
    host = urlparse(base).netloc

    for item in items:
        lines = [f"{item['method']} {base}{item['path']} HTTP/1.1", f'Host: {host}']

        if item.get('token'):
            lines.append(f"Authorization: Bearer {item['token']}")

        payload = ''

        if 'body' in item:
            payload = json.dumps(item['body'])
            lines += ['Content-Type: application/json', f'Content-Length: {len(payload.encode())}']

        zap.core.send_request('\r\n'.join(lines) + '\r\n\r\n' + payload)

    print(f'Replayed {len(items)} API requests through ZAP')

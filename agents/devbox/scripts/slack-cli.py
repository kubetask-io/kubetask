#!/usr/bin/env python3
import os
import sys
import json
import argparse
import urllib.request
import urllib.parse
import urllib.error
import mimetypes

SLACK_API_BASE = "https://slack.com/api"

def get_token():
    token = os.environ.get("SLACK_BOT_TOKEN")
    if not token:
        print("Error: SLACK_BOT_TOKEN environment variable is not set.", file=sys.stderr)
        sys.exit(1)
    return token

def api_call(endpoint, data, token):
    url = f"{SLACK_API_BASE}/{endpoint}"
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json; charset=utf-8"
    }
    
    try:
        req = urllib.request.Request(
            url, 
            data=json.dumps(data).encode('utf-8'), 
            headers=headers
        )
        with urllib.request.urlopen(req) as response:
            resp_body = response.read().decode('utf-8')
            resp_json = json.loads(resp_body)
            if not resp_json.get("ok"):
                print(f"Slack API Error: {resp_json.get('error')}", file=sys.stderr)
                sys.exit(1)
            return resp_json
    except urllib.error.HTTPError as e:
        print(f"HTTP Error: {e.code} {e.reason}", file=sys.stderr)
        print(e.read().decode('utf-8'), file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)

def cmd_send(args):
    token = get_token()
    
    # Get text from args or stdin
    text = args.text
    if not text and not sys.stdin.isatty():
        text = sys.stdin.read()
    
    if not text and not args.blocks:
        print("Error: No text provided. Use --text or pipe via stdin.", file=sys.stderr)
        sys.exit(1)

    payload = {
        "channel": args.channel,
        "text": text
    }
    
    if args.thread_ts:
        payload["thread_ts"] = args.thread_ts
        
    if args.blocks:
        try:
            payload["blocks"] = json.loads(args.blocks)
        except json.JSONDecodeError:
            print("Error: Invalid JSON provided for --blocks", file=sys.stderr)
            sys.exit(1)

    api_call("chat.postMessage", payload, token)

def cmd_upload(args):
    token = get_token()
    
    if not os.path.exists(args.file):
        print(f"Error: File not found: {args.file}", file=sys.stderr)
        sys.exit(1)

    # files.upload is deprecated, but valid for simple use. 
    # For robust zero-dep upload, we use multipart/form-data.
    # Implementing manual multipart in stdlib is verbose, so we use files.upload (v1) 
    # or the new files.getUploadURLExternal flow (v2).
    # For simplicity in a CLI script, v1 is still widely supported for bots.
    
    # Using multipart/form-data with urllib is complex. 
    # To keep this script simple and zero-dep, we construct a manual multipart body.
    
    boundary = '----SlackCLIFormBoundary'
    data = []
    
    # Add token (though header auth is preferred, files.upload sometimes needs it in body for v1)
    # Actually, Bearer header is fine.
    
    # Add channels
    data.append(f'--{boundary}')
    data.append(f'Content-Disposition: form-data; name="channels"')
    data.append('')
    data.append(args.channel)
    
    # Add thread_ts
    if args.thread_ts:
        data.append(f'--{boundary}')
        data.append(f'Content-Disposition: form-data; name="thread_ts"')
        data.append('')
        data.append(args.thread_ts)

    # Add initial_comment
    if args.comment:
        data.append(f'--{boundary}')
        data.append(f'Content-Disposition: form-data; name="initial_comment"')
        data.append('')
        data.append(args.comment)

    # Add file
    filename = os.path.basename(args.file)
    mime_type = mimetypes.guess_type(filename)[0] or 'application/octet-stream'
    
    with open(args.file, 'rb') as f:
        file_content = f.read()
        
    data.append(f'--{boundary}')
    data.append(f'Content-Disposition: form-data; name="file"; filename="{filename}"')
    data.append(f'Content-Type: {mime_type}')
    data.append('')
    # We need to handle mixed str/bytes here, so we'll encode later
    
    # Construct body
    body = b''
    for item in data:
        body += item.encode('utf-8') + b'\r\n'
    
    body += file_content + b'\r\n'
    body += f'--{boundary}--'.encode('utf-8') + b'\r\n'

    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": f"multipart/form-data; boundary={boundary}",
        "Content-Length": len(body)
    }

    try:
        req = urllib.request.Request(
            f"{SLACK_API_BASE}/files.upload", 
            data=body, 
            headers=headers
        )
        with urllib.request.urlopen(req) as response:
            resp_body = response.read().decode('utf-8')
            resp_json = json.loads(resp_body)
            if not resp_json.get("ok"):
                print(f"Slack API Error: {resp_json.get('error')}", file=sys.stderr)
                sys.exit(1)
            print(f"File uploaded: {resp_json.get('file', {}).get('permalink')}")
    except Exception as e:
        print(f"Upload Error: {e}", file=sys.stderr)
        sys.exit(1)

def main():
    parser = argparse.ArgumentParser(description="Slack CLI for KubeOpenCode Agents")
    subparsers = parser.add_subparsers(dest="command", required=True)

    # Command: send
    send_parser = subparsers.add_parser("send", help="Send a message")
    send_parser.add_argument("--channel", required=True, help="Channel ID")
    send_parser.add_argument("--text", help="Message text")
    send_parser.add_argument("--blocks", help="Message blocks (JSON string)")
    send_parser.add_argument("--thread-ts", help="Thread timestamp to reply to")
    send_parser.set_defaults(func=cmd_send)

    # Command: upload
    upload_parser = subparsers.add_parser("upload", help="Upload a file")
    upload_parser.add_argument("--channel", required=True, help="Channel ID")
    upload_parser.add_argument("--file", required=True, help="File path")
    upload_parser.add_argument("--comment", help="Initial comment")
    upload_parser.add_argument("--thread-ts", help="Thread timestamp")
    upload_parser.set_defaults(func=cmd_upload)

    args = parser.parse_args()
    args.func(args)

if __name__ == "__main__":
    main()

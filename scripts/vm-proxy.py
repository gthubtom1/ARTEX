import socket, threading, sys
LISTEN = ("0.0.0.0", 18788)
TARGET = ("127.0.0.1", 8787)

def pipe(a, b):
    try:
        while True:
            d = a.recv(65536)
            if not d: break
            b.sendall(d)
    except Exception: pass
    finally:
        try: a.shutdown(socket.SHUT_RD)
        except Exception: pass

def handle(c):
    try:
        u = socket.create_connection(TARGET, timeout=10)
    except Exception:
        c.close(); return
    threading.Thread(target=pipe, args=(c, u), daemon=True).start()
    threading.Thread(target=pipe, args=(u, c), daemon=True).start()

s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(LISTEN); s.listen(64)
print("proxy up", flush=True)
while True:
    c, _ = s.accept()
    threading.Thread(target=handle, args=(c,), daemon=True).start()

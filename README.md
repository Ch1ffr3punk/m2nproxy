# m2nproxy
mail2news Tor Hidden Service proxy for dizum.com

A .forward file may look like this:
"/home/m2nproxy/./m2nproxy"

TLS certificates can be created with openssl.  
$ openssl req -nodes -new -x509 -keyout key.pem -out cert.pem

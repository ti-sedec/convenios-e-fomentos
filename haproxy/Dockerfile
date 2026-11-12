# HAProxy de PRODUÇÃO com a configuração embutida na imagem.
# Não usamos bind mount de arquivo: no Portainer (stack a partir de repositório) o caminho relativo
# aponta para uma pasta do contêiner do Portainer, não do host, e o HAProxy subiria com um diretório vazio.
FROM haproxy:3.4.7-alpine
COPY haproxy.prod.cfg /usr/local/etc/haproxy/haproxy.cfg
# Valida a configuração no build: um erro de sintaxe derruba o build, não o deploy.
RUN haproxy -c -f /usr/local/etc/haproxy/haproxy.cfg

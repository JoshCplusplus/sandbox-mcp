FROM python:3.13-slim

RUN adduser --home /sandbox --disabled-password sandbox

COPY requirements.txt /tmp
RUN pip install --no-cache-dir -r /tmp/requirements.txt && rm -f /tmp/requirements.txt

# RUN apt-get update && \
#     apt-get install -y iptables iproute2 procps && \
#     rm -rf /var/lib/apt/lists/*


COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
#RUN mkdir -p /var/log/privoxy && chmod 777 /var/log/privoxy

#USER sandbox
WORKDIR /sandbox

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV MPLCONFIGDIR=/tmp
ENV HTTP_PROXY="http://privoxy-proxy:8118" \
    HTTPS_PROXY="http://privoxy-proxy:8118" \
    http_proxy="http://privoxy-proxy:8118" \
    https_proxy="http://privoxy-proxy:8118" \
    NO_PROXY="localhost,127.0.0.1"

ENTRYPOINT ["/entrypoint.sh"]
CMD []
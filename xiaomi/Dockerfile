FROM python:3.15.0-alpine AS build
RUN apk add --update --no-cache gcc musl-dev libffi-dev openssl-dev
COPY . /app/
WORKDIR /app
# --prefix keeps the copy below independent of the Python version.
RUN pip install --no-cache-dir --prefix=/install .

FROM python:3.15.0-alpine
COPY --from=build /install /usr/local
CMD app --ip $IP --port 8000 --token $TOKEN

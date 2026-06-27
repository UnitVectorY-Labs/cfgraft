---
layout: default
title: Installation
nav_order: 2
permalink: /install
---

# Installation
{: .no_toc }

## Table of Contents
{: .no_toc .text-delta }

- TOC
{:toc}

## Installation Methods

There are several ways to install `cfgraft`:

### Download Binary

Download pre-built binaries from the [GitHub Releases](https://github.com/UnitVectorY-Labs/cfgraft/releases) page for the latest version.

[![GitHub release](https://img.shields.io/github/release/UnitVectorY-Labs/cfgraft.svg)](https://github.com/UnitVectorY-Labs/cfgraft/releases/latest) 

Choose the appropriate binary for your platform and add it to your PATH.

### Install Using Go

Install directly from the Go toolchain:

```bash
go install github.com/UnitVectorY-Labs/cfgraft@latest
```

### Build from Source

Build the application from source code:

```bash
git clone https://github.com/UnitVectorY-Labs/cfgraft.git
cd cfgraft
go build -o cfgraft
```

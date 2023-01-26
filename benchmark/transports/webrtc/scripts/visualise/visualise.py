#!/usr/bin/env python3

import sys

import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
import pandas as pd
import argparse

parser = argparse.ArgumentParser(
    description="Plot a time series for the metrics of the given metrics."
)
parser.add_argument("filepath", type=str, nargs='+', help="Path(s) to metrics csv file(s) to visualise.")
parser.add_argument(
    "-s",
    "--streams",
    type=int,
    default=0,
    help="use a value greater then 0 to filter on rows that are recorded with this specific amount of streams active",
)
parser.add_argument(
    "-b",
    "--bucket",
    type=str,
    default='',
    help="bucket the metrics together in bigger chunks (e.g. 30s instead of 1s)",
)
parser.add_argument(
    "-o",
    "--output",
    type=str,
    default='',
    help="output figure to stdout or file (by default it opens in an interactive window)",
)
args = parser.parse_args()

fig, axes = plt.subplots(
    nrows=len(args.filepath),
    ncols=2,
    gridspec_kw={'hspace': 0.5, 'wspace': 0.4},
    figsize=(20, 5 * len(args.filepath) + 1),
    squeeze=False,
)

for row, filepath in enumerate(args.filepath):
    df = pd.read_csv(filepath, header=0, names=["time", "streams", "cpu", "mem", "br", "bw"])
    if args.streams > 0:
        df = df[df.streams == args.streams]
    df['time'] = pd.to_datetime(df['time'], unit='ms')
    df['mem'] = df['mem'] / 1024.0 / 1024.0  # convert to MiB
    df['br'] = df['br'] / 1024.0 # convert to KiB
    df['bw'] = df['bw'] / 1024.0  # convert to KiB
    if args.bucket:
        df = df.resample(args.bucket, on='time').mean()

    if args.bucket:
        axes[row, 0].set_title(f"{filepath} (resampled per {args.bucket} — mean)")
    else:
        axes[row, 0].set_title(filepath)

    ax1 = axes[row, 0]
    ax1.set_ylabel("CPU (%)")
    ax2 = ax1.twinx()
    ax2.set_ylabel("Memory (MiB)")
    df['cpu'].plot(color="red", ax=ax1)
    df['mem'].plot(color="blue", ax=ax2)

    axes[row, 0].set_xlabel("")
    axes[row, 0].grid()
    plt.xticks([])

    axes[row, 1].set_ylabel('KiB')
    df['br'].plot(color="green", ax=axes[row, 1])
    df['bw'].plot(color="yellow", ax=axes[row, 1])

    axes[row, 1].set_xlabel("")
    axes[row, 1].grid()
    plt.xticks([])

cpu_label = mpatches.Patch(color='red', label='CPU (%)')
mem_label = mpatches.Patch(color='blue', label="Memory (MiB)")
br_label = mpatches.Patch(color='green', label="Read Throughput (KiB)")
bw_label = mpatches.Patch(color='yellow', label="Write Throughput (KiB)")
fig.legend(handles=[cpu_label, mem_label, br_label, bw_label], ncol=5)

if args.output:
    if args.output.lower() == 'stdout':
        plt.savefig(sys.stdout)
    else:
        plt.savefig(args.output)
else:
    plt.show()



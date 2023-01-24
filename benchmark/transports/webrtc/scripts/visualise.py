#!/usr/bin/env python3

import matplotlib.pyplot as plt
import matplotlib.patches as mpatches
import pandas as pd
import argparse

parser = argparse.ArgumentParser(
    description="Plot a time series for the metrics of the given metrics."
)
parser.add_argument("filepath", type=str, help="Path to metrics csv file to visualise.")
parser.add_argument(
    "-s",
    "--streams",
    type=int,
    default=0,
    help="use a value greater then 0 to filter on rows that are recorded with this specific amount of streams active",
)
args = parser.parse_args()

df = pd.read_csv(args.filepath, header=0, names=["time", "streams", "cpu", "mem"])
df['mem'] = df['mem'] / 1024.0 / 1024.0  # convert to MiB

if args.streams > 0:
    df = df[df.streams == args.streams]

df['cpu'].plot(kind='bar', color="red")
plt.ylim(0, 20)

df['mem'].plot( color="blue", secondary_y=True)
plt.ylim(0, 120)

plt.xticks([])

cpu_label = mpatches.Patch(color='red', label='CPU (%)')
mem_label = mpatches.Patch(color='blue', label="Memory (MiB)")
plt.legend(handles=[cpu_label, mem_label])

plt.show()



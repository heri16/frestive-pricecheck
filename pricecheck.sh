#!/bin/bash
cd "$(dirname "$0")"

PATH_BARCODE_SCANNER="/dev/serial/by-id/usb-Datalogic_ADC__Inc._Handheld_Barcode_Scanner_S_N_G14F13974-if00"
PATH_POLE_DISPLAY="/dev/serial/by-id/usb-0*_*-if0*"

echo "Starting up..."

COUNTER=1
while [ $COUNTER -lt 61 ]; do
  # Resolve wildcards in file path (in each iteration)
  PATH_BARCODE_SCANNER=($PATH_BARCODE_SCANNER)
  PATH_POLE_DISPLAY=($PATH_POLE_DISPLAY)

  # Check if path is symbolic link
  if [ -L "$PATH_BARCODE_SCANNER" ] && [ -L "$PATH_POLE_DISPLAY" ] ; then
    # Resolve symbolic links
    export PORT_BARCODE_SCANNER="$(realpath "$PATH_BARCODE_SCANNER")"
    export PORT_POLE_DISPLAY="$(realpath "$PATH_POLE_DISPLAY")"

    # Check if port is character device
    if [ -c "$PORT_BARCODE_SCANNER" ] && [ -c "$PORT_POLE_DISPLAY" ] ; then
      # Set to raw mode (-icrnl) to prevent any newline translation
      stty -F $PORT_BARCODE_SCANNER raw
      stty -F $PORT_POLE_DISPLAY raw
      ./pricecheck
    else
      echo "$PORT_BARCODE_SCANNER or $PORT_POLE_DISPLAY is not found (not a character device)."
    fi

  else
    echo "$PATH_BARCODE_SCANNER or $PATH_POLE_DISPLAY is not found (not a symbolic link)."
  fi

  echo Restart $COUNTER
  sleep 15s
  let COUNTER=COUNTER+1
done

reboot